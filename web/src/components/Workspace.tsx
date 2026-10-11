import { useEffect, useRef, useState, type FormEvent } from 'react'
import type { Api } from '../api'
import type { Answer, Metric, Role, User } from '../types'
import { AnswerCard } from './AnswerCard'

const EXAMPLES: Record<Role, string[]> = {
  viewer: [
    'What was MRR last month?',
    'Show MRR by plan for the last 6 months',
    'How did support tickets change in 2025?',
    'What is our gross margin?',
  ],
  analyst: [
    'Compare new and churned customers this year',
    'CSAT over the past 12 months',
    'select plan, round(avg(mrr_usd), 2) as avg_mrr from gold.revenue_daily group by plan limit 10',
    'select a.date, b.plan, a.mrr_usd, b.mrr_usd from gold.revenue_daily a cross join gold.revenue_daily b limit 1000',
  ],
  owner: [
    'Gross margin over the last 12 months',
    'infra cost per region since March 2026',
    'select month, revenue_usd, cost_usd from gold.finance_monthly limit 12',
    'What was MRR last month?',
  ],
}

const ROLE_NOTE: Record<Role, string> = {
  viewer: 'Metrics only. No SQL, no finance or infrastructure data.',
  analyst: 'Metrics and read-only SQL. No finance or infrastructure data.',
  owner: 'Everything, finance and infrastructure included.',
}

interface Entry {
  id: number
  question: string
  answer?: Answer
  error?: string
  pending: boolean
}

type Props = { api: Api; user: User; onSignOut: () => void }

export function Workspace({ api, user, onSignOut }: Props) {
  const [entries, setEntries] = useState<Entry[]>([])
  const [question, setQuestion] = useState('')
  const [metrics, setMetrics] = useState<Metric[]>([])
  const [approving, setApproving] = useState<string | null>(null)
  const nextId = useRef(1)
  const bottom = useRef<HTMLDivElement>(null)

  useEffect(() => {
    api.catalog().then((c) => setMetrics(c.metrics), () => setMetrics([]))
  }, [api])

  useEffect(() => {
    bottom.current?.scrollIntoView?.({ behavior: 'smooth', block: 'end' })
  }, [entries])

  function update(id: number, patch: Partial<Entry>) {
    setEntries((es) => es.map((e) => (e.id === id ? { ...e, ...patch } : e)))
  }

  async function ask(text: string) {
    const q = text.trim()
    if (!q) return
    const id = nextId.current++
    setEntries((es) => [...es, { id, question: q, pending: true }])
    setQuestion('')
    try {
      update(id, { answer: await api.chat(q), pending: false })
    } catch (err) {
      update(id, { error: err instanceof Error ? err.message : 'Request failed', pending: false })
    }
  }

  async function approve(entryId: number, token: string) {
    setApproving(token)
    try {
      const result = await api.approve(token)
      setEntries((es) => {
        const done = es.map((e) => (e.id === entryId && e.answer ? { ...e, answer: { ...e.answer, approval: undefined } } : e))
        return [...done, { id: nextId.current++, question: 'Approved query', answer: result, pending: false }]
      })
    } catch (err) {
      update(entryId, { error: err instanceof Error ? err.message : 'Approval failed' })
    } finally {
      setApproving(null)
    }
  }

  function submit(ev: FormEvent) {
    ev.preventDefault()
    ask(question)
  }

  return (
    <div className="workspace">
      <aside className="rail">
        <p className="eyebrow">Northwind Cloud · analytics</p>
        <div className="who">
          <span className={`role role-${user.role}`}>{user.role}</span>
          <span className="email">{user.email}</span>
          <p>{ROLE_NOTE[user.role]}</p>
          <button type="button" className="link" onClick={onSignOut}>
            Sign out
          </button>
        </div>
        <h2>Try</h2>
        <ul className="examples">
          {EXAMPLES[user.role].map((ex) => (
            <li key={ex}>
              <button type="button" onClick={() => ask(ex)}>
                {ex}
              </button>
            </li>
          ))}
        </ul>
        {metrics.length > 0 && (
          <>
            <h2>Metrics you can ask about</h2>
            <ul className="metrics">
              {metrics.map((m) => (
                <li key={m.name} title={m.description}>
                  <code>{m.name}</code>
                  <span>{m.unit}</span>
                </li>
              ))}
            </ul>
          </>
        )}
      </aside>

      <main className="conversation">
        {entries.length === 0 && (
          <div className="empty">
            <h1>Ask the warehouse.</h1>
            <p>Questions in plain English. Every answer shows the tools it used and what your role allowed.</p>
          </div>
        )}
        {entries.map((e) => (
          <section key={e.id} className="entry" aria-busy={e.pending}>
            <p className="question">{e.question}</p>
            {e.pending && <p className="thinking">Working on it…</p>}
            {e.answer && (
              <AnswerCard answer={e.answer} onApprove={(tok) => approve(e.id, tok)} busy={approving !== null} />
            )}
            {e.error && (
              <p className="error" role="alert">
                {e.error}
              </p>
            )}
          </section>
        ))}
        <div ref={bottom} />
        <form className="ask" onSubmit={submit}>
          <label htmlFor="question" className="visually-hidden">
            Your question
          </label>
          <input
            id="question"
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
            placeholder="e.g. How did MRR change this year?"
            maxLength={2000}
            autoComplete="off"
          />
          <button type="submit" disabled={!question.trim()}>
            Ask
          </button>
        </form>
      </main>
    </div>
  )
}
