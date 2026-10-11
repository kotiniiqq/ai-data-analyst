import type { Answer, Metric, User } from './types'

export interface Api {
  login(email: string, password: string): Promise<{ token: string; user: User }>
  chat(message: string): Promise<Answer>
  approve(token: string): Promise<Answer>
  catalog(): Promise<{ metrics: Metric[] }>
}

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code: string,
  ) {
    super(message)
  }
}

/** HTTP client for the analyst API. getToken returns the session token, if any. */
export function httpApi(getToken: () => string | null, onUnauthorized: () => void): Api {
  async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' }
    const token = getToken()
    if (token) headers.Authorization = `Bearer ${token}`
    const res = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) })
    const data = await res.json().catch(() => ({}))
    if (!res.ok) {
      if (res.status === 401 && token) onUnauthorized()
      throw new ApiError(data?.error?.msg ?? `Request failed (${res.status})`, res.status, data?.error?.code ?? 'error')
    }
    return data as T
  }
  return {
    login: (email, password) => call('POST', '/api/login', { email, password }),
    chat: (message) => call('POST', '/api/chat', { message }),
    approve: (token) => call('POST', `/api/approvals/${encodeURIComponent(token)}`),
    catalog: () => call('GET', '/api/catalog'),
  }
}
