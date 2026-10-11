# ai-data-analyst

Ask a data warehouse questions in plain English and get an answer, a table, a chart, and the trace of
how the answer was found. The assistant can act only through tools that the user's role allows. Every
SQL query passes a validator and a cost check, every tool call is audited, and the guard rejects an
answer that quotes a number no tool returned.

Go (Fiber) API, React client, Postgres or BigQuery as the warehouse. It runs on a laptop **without any
API keys**.

> Reference version of the AI analyst I built into a client's analytics dashboard (Go, React,
> BigQuery). That system is private. This repo rebuilds the idea from scratch on a synthetic
> warehouse of "Northwind Cloud", a made-up SaaS company with three plans.

## Run it

Needs Docker.

```bash
git clone https://github.com/kotiniiqq/ai-data-analyst && cd ai-data-analyst
docker compose up -d --build --wait
```

Open http://localhost:8088 and pick a demo user (local only):

| user | password | role | can |
|---|---|---|---|
| `viewer@demo.local` | `viewer-demo` | viewer | ask about metrics |
| `analyst@demo.local` | `analyst-demo` | analyst | + see and run read-only SQL |
| `owner@demo.local` | `owner-demo` | owner | + finance and infrastructure data |

Questions to try: *What was MRR last month?* · *Show MRR by plan for the last 6 months* · *What is our
gross margin?* (as viewer, then as owner) · *Compare new and churned customers this year* · or raw SQL
as analyst: `select plan, round(avg(mrr_usd), 2) from gold.revenue_daily group by plan limit 10`.

The API is on http://localhost:8080 too:

```bash
TOKEN=$(curl -s localhost:8080/api/login -H 'content-type: application/json' \
  -d '{"email":"analyst@demo.local","password":"analyst-demo"}' | jq -r .token)
curl -s localhost:8080/api/chat -H "authorization: Bearer $TOKEN" -H 'content-type: application/json' \
  -d '{"message":"What was MRR last month?"}' | jq '{answer, sql, warnings}'
```

```json
{
  "answer": "MRR in Sep 2026 was $284,830.27.",
  "sql": "SELECT to_char(date_trunc('month', date), 'YYYY-MM') AS month, round(sum(mrr_usd) / count(DISTINCT date), 2) AS mrr FROM gold.revenue_daily WHERE date BETWEEN DATE '2026-09-01' AND DATE '2026-09-30' GROUP BY 1 ORDER BY 1 LIMIT 1000",
  "warnings": []
}
```

## How a question is answered

```
question ─► planner ─► tool call ─► registry ─────────────► tool ─► answer + trace
            (demo or     (name,      role allows it?          │
             a model)     args)      args valid?              ├─ metric_series: SQL built from the catalog
                                     audit (always)           └─ run_sql: the analyst's SQL
                                                                     │
                                          SQL guard ─► cost estimate ─► run, or hold for approval
                                                                     │
                                          read-only warehouse role (schema gold, 10 s timeout)

final answer ─► number guard: every number must come from a tool, otherwise the data is shown as is
```

- **The model never touches data directly.** It sees only the tools the user's role allows
  (`list_metrics`, `describe_metric`, `metric_series`, `list_tables`, `describe_table`, `run_sql`).
  The role comes from the signed session token, so nothing typed into the chat can change it.
- **Metrics come from a catalog.** `metric_series` builds its SQL from metric definitions (table,
  expression, allowed splits, sensitivity), and that SQL goes through the same guard as hand-written SQL.
  The tool also returns first, last, change, change %, min, max, average and total, computed in code,
  so the model has no reason to do arithmetic.
- **SQL guard** works on tokens, not regular expressions. It understands strings, `E''` escapes,
  dollar quotes, quoted identifiers and nested comments. A query must be one SELECT (or WITH … SELECT)
  that reads only catalog tables in schema `gold`. Sensitive tables need `data.sensitive`, and `SELECT *`
  on them is refused. LIMIT is required and capped at 1000. `pg_*`, `lo_*`, `dblink*`, `*_to_xml`,
  `set_config` and similar functions are refused. Comments are stripped before the query runs.
- **Cost check.** Postgres: `EXPLAIN`, and the estimate is the largest rows × width of any plan node.
  BigQuery: a dry run (bytes billed). At 100 MiB or more (`APPROVAL_BYTES`) the query is held. The user
  sees the estimate and the price at $6.25/TiB and clicks to run it. The approval token belongs to that
  user, works once and expires after 10 minutes. No planner can approve a query.
- **Audit log.** Each call records the user, the tool, whether it was allowed, the outcome code, the
  argument *names* and the duration. Argument values (questions, SQL) are never stored.
- **Number guard.** Before the final answer is shown, every number in it is checked against the numbers
  the tools returned. Rounding, `k`/`M`, `%` and signs are allowed, and so are years and small counts.
  If the answer quotes anything else, the user gets the tool output itself and a warning.

## Modes

| | default | with keys |
|---|---|---|
| planner | `PLANNER=demo`: rule-based. Understands metric names and synonyms, periods (last month, last N months, Q2 2026, since March 2026, a year), splits by plan or region, comparisons, and raw SQL | `PLANNER=openrouter` + `OPENROUTER_API_KEY`: a real model with tool calling over the same tools (`OPENROUTER_MODEL`, default `anthropic/claude-sonnet-5`) |
| warehouse | Postgres with the synthetic `gold` schema | `WAREHOUSE=bigquery` + `BIGQUERY_PROJECT` + Google application default credentials |

Copy `.env.example` to `.env` to switch. The OpenRouter and BigQuery clients are tested against
recorded API shapes (`httptest`), not against the live services. BigQuery mode expects a dataset
called `gold` with the same tables.

## Tests

| | what | count |
|---|---|---|
| Go (`scripts/test.sh`) | SQL guard (49 tricky queries), roles and JWT, catalog, cost estimate (EXPLAIN fixtures and the real database), approvals, warehouse providers, tools and audit, agent loop, number guard, demo planner (golden questions), OpenRouter client, HTTP API, config | 97 tests, 146 with subtests |
| Web (`cd web && npm test`) | components and the chat flow | 12 |
| End to end (`node e2e/run.mjs`) | on the running stack: viewer refused gross margin and the denial audited, no values in the audit, analyst's MRR equals a direct DB query, viewer never sees SQL, viewer cannot run SQL, a second statement is rejected and nothing is dropped, analyst cannot read the users table, a wide query waits for approval and only its owner can approve it once, owner's gross margin equals the DB | 10 checks |

`scripts/test.sh` runs the Go toolchain in Docker, so you don't need Go installed. Start the
database first: `docker compose up -d --wait db`. CI runs all three suites.

## Layout

```
cmd/api, cmd/seed       API server; demo users
internal/
  auth, rbac            login, JWT (HS256, 8 h), roles → permissions
  catalog               tables, metrics, sensitivity
  sqlguard              lexer + rules
  cost, approval        estimates (EXPLAIN / dry run), held queries
  warehouse, bq         Postgres (read-only role) and BigQuery providers
  tools, audit          tool registry, built-in tools, audit log
  agent                 question loop, number guard, chart data
  planner               demo planner, OpenRouter planner
  httpapi, config       routes, settings
db/migrations           app tables, synthetic warehouse, read-only role
web/                    React client (Vite), served by nginx
e2e/run.mjs             end-to-end checks
```

## Limits

- The demo planner is rule-based: it answers the kinds of questions listed above and nothing else.
  It does one round of tool calls and then writes the answer.
- The SQL guard is the first line of defence. The second is the database role: it can only `SELECT`
  from schema `gold`, with a 10-second statement timeout.
- Login has no rate limit and the ports bind to 127.0.0.1. This is a local demo, not a hosted service.

## License

MIT
