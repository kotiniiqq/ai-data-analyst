// End-to-end checks against the running compose stack (docker compose up -d --build).
// Usage: node e2e/run.mjs [baseUrl]   (default http://localhost:8080)
import { execFileSync } from 'node:child_process'
import assert from 'node:assert/strict'

const base = process.argv[2] ?? 'http://localhost:8080'
let failures = 0

function sql(query) {
  return execFileSync('docker', ['compose', 'exec', '-T', 'db', 'psql', '-U', 'app', '-d', 'analyst', '-tAc', query], {
    encoding: 'utf8',
  }).trim()
}

async function call(method, path, token, body) {
  const res = await fetch(base + path, {
    method,
    headers: { 'content-type': 'application/json', ...(token ? { authorization: `Bearer ${token}` } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  return { status: res.status, body: await res.json().catch(() => ({})) }
}

async function login(role) {
  const r = await call('POST', '/api/login', null, { email: `${role}@demo.local`, password: `${role}-demo` })
  assert.equal(r.status, 200, `login ${role}`)
  return r.body.token
}

const ask = (token, message) => call('POST', '/api/chat', token, { message })

async function check(name, fn) {
  try {
    await fn()
    console.log(`ok    ${name}`)
  } catch (err) {
    failures++
    console.log(`FAIL  ${name}\n      ${err.message.split('\n').join('\n      ')}`)
  }
}

for (let i = 0; ; i++) {
  const ok = await fetch(base + '/health').then((r) => r.ok, () => false)
  if (ok) break
  if (i === 60) throw new Error('the API did not become healthy in 60 s')
  await new Promise((r) => setTimeout(r, 1000))
}

const startedAt = sql('select now()')
const viewer = await login('viewer')
const analyst = await login('analyst')
const owner = await login('owner')

await check('health', async () => {
  assert.equal((await call('GET', '/health')).status, 200)
})

await check('viewer is refused gross margin, and the denial is audited', async () => {
  const r = await ask(viewer, 'What is our gross margin?')
  assert.equal(r.status, 200)
  assert.match(r.body.answer, /^I can't share that/)
  assert.equal(r.body.trace[0].allowed, false)
  const denied = sql(
    `select count(*) from audit_log a join users u on u.id = a.user_id
     where u.email = 'viewer@demo.local' and a.tool = 'metric_series' and not a.allowed and a.code = 'forbidden' and a.at >= '${startedAt}'`,
  )
  assert.ok(Number(denied) >= 1, `denied audit rows: ${denied}`)
})

await check('audit stores input names, never values', async () => {
  const leaked = sql(`select count(*) from audit_log where array_to_string(input_keys, ',') ilike '%gross%' or array_to_string(input_keys, ',') ilike '%select%'`)
  assert.equal(leaked, '0')
})

await check("analyst's MRR for last month equals a direct database query", async () => {
  const r = await ask(analyst, 'What was MRR last month?')
  const direct = sql(`select round(sum(mrr_usd) / count(distinct date), 2) from gold.revenue_daily where date between '2026-09-01' and '2026-09-30'`)
  assert.equal(r.body.table.rows[0][1], Number(direct))
  assert.ok(r.body.answer.includes(Number(direct).toLocaleString('en-US')), r.body.answer)
  assert.deepEqual(r.body.warnings, [])
  assert.match(r.body.sql, /^SELECT/)
})

await check('viewer never sees SQL', async () => {
  const r = await ask(viewer, 'Show MRR by plan for the last 6 months')
  assert.equal(r.body.sql, undefined)
  assert.ok(r.body.chart && r.body.chart.series.length === 3)
})

await check('viewer cannot run SQL', async () => {
  const r = await ask(viewer, 'select plan from gold.revenue_daily limit 1')
  assert.equal(r.body.trace[0].code, 'forbidden')
  assert.equal(r.body.table, undefined)
})

await check('a second statement is rejected and nothing is dropped', async () => {
  const r = await ask(analyst, 'select 1 from gold.revenue_daily limit 1; drop table gold.revenue_daily')
  assert.equal(r.body.trace[0].code, 'sql_rejected')
  assert.equal(sql('select count(*) from gold.revenue_daily'), '3012')
})

await check('analyst cannot read the users table', async () => {
  const r = await ask(analyst, 'select email, password_hash from public.users limit 5')
  assert.equal(r.body.trace[0].code, 'sql_rejected')
})

await check('a wide query waits for approval; only its owner can approve it, once', async () => {
  const r = await ask(analyst, 'select a.date, b.plan, a.mrr_usd, b.mrr_usd from gold.revenue_daily a cross join gold.revenue_daily b limit 5000')
  const ap = r.body.approval
  assert.ok(ap && ap.estimate.needs_approval, JSON.stringify(r.body))
  assert.equal(r.body.table, undefined)
  assert.equal((await call('POST', `/api/approvals/${ap.token}`, viewer)).status, 403)
  const ok = await call('POST', `/api/approvals/${ap.token}`, analyst)
  assert.equal(ok.status, 200)
  assert.equal(ok.body.table.rows.length, 1000)
  assert.equal((await call('POST', `/api/approvals/${ap.token}`, analyst)).status, 404)
})

await check("owner's gross margin equals the database", async () => {
  const r = await ask(owner, 'Gross margin in September 2026')
  const direct = sql(`select gross_margin_pct from gold.finance_monthly where month = '2026-09-01'`)
  assert.equal(r.body.table.rows[0][1], Number(direct))
  assert.ok(r.body.answer.includes(`${Number(direct)}%`), r.body.answer)
})

console.log(failures ? `\n${failures} check(s) failed` : '\nall checks passed')
process.exit(failures ? 1 : 0)
