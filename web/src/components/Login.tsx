import { useState, type FormEvent } from 'react'

const DEMO = [
  { role: 'Viewer', email: 'viewer@demo.local', password: 'viewer-demo', can: 'Metrics only' },
  { role: 'Analyst', email: 'analyst@demo.local', password: 'analyst-demo', can: 'Metrics and SQL' },
  { role: 'Owner', email: 'owner@demo.local', password: 'owner-demo', can: 'Everything, finance included' },
]

export function Login({ onLogin }: { onLogin: (email: string, password: string) => Promise<void> }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function signIn(e: string, p: string) {
    setBusy(true)
    setError('')
    try {
      await onLogin(e, p)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Sign-in failed')
    } finally {
      setBusy(false)
    }
  }

  function submit(ev: FormEvent) {
    ev.preventDefault()
    signIn(email, password)
  }

  return (
    <main className="login">
      <header>
        <p className="eyebrow">Northwind Cloud · analytics</p>
        <h1>Ask the warehouse.</h1>
        <p className="lede">
          Pick a demo user. Each has a different role, and the assistant can only do what that role allows.
        </p>
      </header>
      <div className="demo-users">
        {DEMO.map((d) => (
          <button key={d.role} type="button" disabled={busy} onClick={() => signIn(d.email, d.password)}>
            <strong>{d.role}</strong>
            <span>{d.can}</span>
          </button>
        ))}
      </div>
      <form onSubmit={submit}>
        <label>
          Email
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="username" required />
        </label>
        <label>
          Password
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </label>
        <button type="submit" disabled={busy}>
          Sign in
        </button>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
      </form>
    </main>
  )
}
