package tools

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kotiniiqq/ai-data-analyst/internal/approval"
	"github.com/kotiniiqq/ai-data-analyst/internal/audit"
	"github.com/kotiniiqq/ai-data-analyst/internal/catalog"
	"github.com/kotiniiqq/ai-data-analyst/internal/cost"
	"github.com/kotiniiqq/ai-data-analyst/internal/rbac"
	"github.com/kotiniiqq/ai-data-analyst/internal/warehouse"
)

var (
	viewer  = rbac.NewUser(1, "viewer@demo.local", "viewer")
	analyst = rbac.NewUser(2, "analyst@demo.local", "analyst")
	owner   = rbac.NewUser(3, "owner@demo.local", "owner")
	t0      = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
)

type fakeWarehouse struct {
	dialect string
	sqls    []string
	rows    warehouse.Rows
	err     error
}

func (f *fakeWarehouse) Dialect() string { return f.dialect }
func (f *fakeWarehouse) Query(_ context.Context, sql string) (warehouse.Rows, error) {
	f.sqls = append(f.sqls, sql)
	return f.rows, f.err
}

type fakeEstimator struct{ bytes int64 }

func (f fakeEstimator) Estimate(context.Context, string) (cost.Estimate, error) {
	return cost.FromBytes(f.bytes, 100<<20), nil
}

type env struct {
	reg   *Registry
	wh    *fakeWarehouse
	log   *audit.Memory
	store *approval.Store
}

func newEnv(t *testing.T, bytes int64) env {
	t.Helper()
	wh := &fakeWarehouse{dialect: "postgres", rows: warehouse.Rows{
		Columns: []string{"month", "mrr"},
		Rows:    [][]any{{"2026-07", 280000.5}, {"2026-08", 281694.59}, {"2026-09", 284830.27}},
	}}
	log := &audit.Memory{}
	store := approval.NewStore()
	b := &Builtins{
		Catalog: catalog.Default(), Warehouse: wh, Estimator: fakeEstimator{bytes}, Approvals: store,
		Now: func() time.Time { return t0 }, DataStart: "2024-01-01", DataEnd: "2026-09-30",
	}
	return env{reg: NewRegistry(log, b), wh: wh, log: log, store: store}
}

func toolCode(err error) string {
	var te *ToolError
	if errors.As(err, &te) {
		return te.Code
	}
	if err != nil {
		return "other:" + err.Error()
	}
	return ""
}

func TestViewerCannotRunSQLAndTheDenialIsAudited(t *testing.T) {
	e := newEnv(t, 1000)
	_, err := e.reg.Execute(context.Background(), viewer, "run_sql", map[string]any{"sql": "select 1 from gold.support_daily limit 1"})
	if toolCode(err) != "forbidden" {
		t.Fatalf("got %v", err)
	}
	if len(e.wh.sqls) != 0 {
		t.Fatal("query reached the warehouse")
	}
	got := e.log.Entries()
	if len(got) != 1 || got[0].Allowed || got[0].Code != "forbidden" || got[0].UserID != 1 || got[0].Tool != "run_sql" {
		t.Fatalf("audit %+v", got)
	}
}

func TestAuditKeepsInputKeysNotValues(t *testing.T) {
	e := newEnv(t, 1000)
	e.reg.Execute(context.Background(), analyst, "run_sql", map[string]any{"sql": "select 'secret-value' from gold.support_daily limit 1"})
	got := e.log.Entries()
	if len(got) != 1 || !slices.Equal(got[0].InputKeys, []string{"sql"}) || !got[0].Allowed || got[0].Code != "" {
		t.Fatalf("audit %+v", got)
	}
}

func TestSchemasListOnlyPermittedTools(t *testing.T) {
	e := newEnv(t, 1000)
	var names []string
	for _, tl := range e.reg.Schemas(viewer) {
		names = append(names, tl.Name)
	}
	if slices.Contains(names, "run_sql") || !slices.Contains(names, "metric_series") {
		t.Fatalf("viewer tools %v", names)
	}
}

func TestUnknownToolAndBadInput(t *testing.T) {
	e := newEnv(t, 1000)
	ctx := context.Background()
	if _, err := e.reg.Execute(ctx, owner, "drop_everything", nil); toolCode(err) != "unknown_tool" {
		t.Fatalf("unknown tool: %v", err)
	}
	cases := []map[string]any{
		{},                              // metric missing
		{"metric": 5},                   // wrong type
		{"metric": "mrr", "extra": "x"}, // unknown key
		{"metric": "mrr", "from": "2026-13-01", "to": "2026-09-30"},
		{"metric": "mrr", "from": "2026-09-01", "to": "2026-01-01"},
		{"metric": "mrr", "from": "2027-01-01", "to": "2027-06-30"}, // no data there
		{"metric": "mrr", "from": "2026-01-01", "to": "2026-09-30", "by": "region"},
	}
	for _, args := range cases {
		if _, err := e.reg.Execute(ctx, analyst, "metric_series", args); toolCode(err) != "bad_input" {
			t.Fatalf("%v: got %v", args, err)
		}
	}
	if len(e.wh.sqls) != 0 {
		t.Fatal("bad input reached the warehouse")
	}
}

func TestMetricSeriesUnknownAndForbiddenMetrics(t *testing.T) {
	e := newEnv(t, 1000)
	ctx := context.Background()
	args := map[string]any{"metric": "happiness", "from": "2026-01-01", "to": "2026-09-30"}
	if _, err := e.reg.Execute(ctx, analyst, "metric_series", args); toolCode(err) != "unknown_metric" {
		t.Fatalf("got %v", err)
	}
	args["metric"] = "gross_margin_pct"
	if _, err := e.reg.Execute(ctx, analyst, "metric_series", args); toolCode(err) != "forbidden" {
		t.Fatalf("got %v", err)
	}
	if _, err := e.reg.Execute(ctx, owner, "metric_series", args); err != nil {
		t.Fatalf("owner: %v", err)
	}
}

func TestMetricSeriesBuildsGuardedSQLAndSummarises(t *testing.T) {
	e := newEnv(t, 1000)
	out, err := e.reg.Execute(context.Background(), viewer, "metric_series",
		map[string]any{"metric": "MRR", "from": "2026-07-01", "to": "2026-09-30"})
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT to_char(date_trunc('month', date), 'YYYY-MM') AS month, round(sum(mrr_usd) / count(DISTINCT date), 2) AS mrr " +
		"FROM gold.revenue_daily WHERE date BETWEEN DATE '2026-07-01' AND DATE '2026-09-30' GROUP BY 1 ORDER BY 1 LIMIT 1000"
	if len(e.wh.sqls) != 1 || e.wh.sqls[0] != want {
		t.Fatalf("sql:\n%s\nwant:\n%s", e.wh.sqls, want)
	}
	if out.SQL != want || out.Rows == nil || len(out.Rows.Rows) != 3 {
		t.Fatalf("output %+v", out)
	}
	s := out.Data.(SeriesSummary)
	if s.Metric != "mrr" || s.Unit != "USD" || s.First != 280000.5 || s.Last != 284830.27 || s.Change != 4829.77 || s.ChangePct != 1.72 {
		t.Fatalf("summary %+v", s)
	}
	for _, n := range []float64{280000.5, 284830.27, 4829.77, 1.72, 281694.59} {
		if !slices.Contains(out.Numbers, n) {
			t.Fatalf("number %v missing from %v", n, out.Numbers)
		}
	}
}

func TestMetricSeriesByPlanAndBigQueryDialect(t *testing.T) {
	e := newEnv(t, 1000)
	e.wh.dialect = "bigquery"
	e.wh.rows = warehouse.Rows{Columns: []string{"month", "plan", "mrr"}, Rows: [][]any{{"2026-09", "team", 75000.0}}}
	_, err := e.reg.Execute(context.Background(), viewer, "metric_series",
		map[string]any{"metric": "mrr", "from": "2026-09-01", "to": "2026-09-30", "by": "plan"})
	if err != nil {
		t.Fatal(err)
	}
	got := e.wh.sqls[0]
	if !strings.HasPrefix(got, "SELECT FORMAT_DATE('%Y-%m', DATE_TRUNC(date, MONTH)) AS month, plan, ") || !strings.Contains(got, "GROUP BY 1, 2 ORDER BY 1, 2") {
		t.Fatalf("sql %s", got)
	}
}

func TestRunSQLRejectedByGuard(t *testing.T) {
	e := newEnv(t, 1000)
	_, err := e.reg.Execute(context.Background(), analyst, "run_sql", map[string]any{"sql": "select * from public.users limit 1"})
	if toolCode(err) != "sql_rejected" || !strings.Contains(err.Error(), "unknown_table") {
		t.Fatalf("got %v", err)
	}
}

func TestExpensiveQueryWaitsForApproval(t *testing.T) {
	e := newEnv(t, 500<<20)
	ctx := context.Background()
	out, err := e.reg.Execute(ctx, analyst, "run_sql", map[string]any{"sql": "select date from gold.revenue_daily limit 5000"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Approval == nil || out.Approval.Token == "" || !out.Approval.Estimate.NeedsApproval || len(e.wh.sqls) != 0 {
		t.Fatalf("output %+v, ran %v", out, e.wh.sqls)
	}
	if out.Approval.SQL != "select date from gold.revenue_daily limit 1000" {
		t.Fatalf("pending sql %q", out.Approval.SQL)
	}
	// another user cannot use it
	if _, err := e.reg.RunApproved(ctx, viewer, out.Approval.Token); toolCode(err) != "forbidden" {
		t.Fatalf("viewer approval: %v", err)
	}
	ran, err := e.reg.RunApproved(ctx, analyst, out.Approval.Token)
	if err != nil || ran.Rows == nil || len(e.wh.sqls) != 1 {
		t.Fatalf("approved run: %+v %v", ran, err)
	}
	if _, err := e.reg.RunApproved(ctx, analyst, out.Approval.Token); toolCode(err) != "not_found" {
		t.Fatalf("reuse: %v", err)
	}
	last := e.log.Entries()[len(e.log.Entries())-1]
	if last.Tool != "run_sql_approved" {
		t.Fatalf("approval not audited: %+v", last)
	}
}

func TestWarehouseErrorBecomesToolError(t *testing.T) {
	e := newEnv(t, 1000)
	e.wh.err = &warehouse.Error{Code: "timeout", Msg: "the query took too long and was stopped"}
	_, err := e.reg.Execute(context.Background(), analyst, "run_sql", map[string]any{"sql": "select 1 from gold.support_daily limit 1"})
	if toolCode(err) != "warehouse" || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("got %v", err)
	}
}

func TestDescribeToolsRespectSensitivity(t *testing.T) {
	e := newEnv(t, 1000)
	ctx := context.Background()
	if _, err := e.reg.Execute(ctx, analyst, "describe_table", map[string]any{"table": "gold.finance_monthly"}); toolCode(err) != "forbidden" {
		t.Fatalf("got %v", err)
	}
	if _, err := e.reg.Execute(ctx, analyst, "describe_metric", map[string]any{"metric": "infra_cost"}); toolCode(err) != "forbidden" {
		t.Fatalf("got %v", err)
	}
	out, err := e.reg.Execute(ctx, analyst, "list_tables", nil)
	if err != nil || strings.Contains(out.Text, "finance") {
		t.Fatalf("list_tables: %q %v", out.Text, err)
	}
	out, err = e.reg.Execute(ctx, viewer, "list_metrics", nil)
	if err != nil || !strings.Contains(out.Text, "mrr") || strings.Contains(out.Text, "gross_margin") {
		t.Fatalf("list_metrics: %q %v", out.Text, err)
	}
}
