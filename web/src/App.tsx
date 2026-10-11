import { useMemo, useState } from 'react'
import { httpApi } from './api'
import type { User } from './types'
import { Login } from './components/Login'
import { Workspace } from './components/Workspace'

const KEY = 'analyst-session'

interface Session {
  token: string
  user: User
}

function load(): Session | null {
  try {
    return JSON.parse(sessionStorage.getItem(KEY) ?? 'null')
  } catch {
    return null
  }
}

export function App() {
  const [session, setSession] = useState<Session | null>(load)

  const api = useMemo(
    () =>
      httpApi(
        () => session?.token ?? null,
        () => signOut(),
      ),
    [session],
  )

  function signOut() {
    sessionStorage.removeItem(KEY)
    setSession(null)
  }

  async function login(email: string, password: string) {
    const s = await api.login(email, password)
    sessionStorage.setItem(KEY, JSON.stringify(s))
    setSession(s)
  }

  return session ? <Workspace api={api} user={session.user} onSignOut={signOut} /> : <Login onLogin={login} />
}
