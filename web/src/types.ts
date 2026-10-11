export type Role = 'viewer' | 'analyst' | 'owner'

export interface User {
  id: number
  email: string
  role: Role
  perms: string[]
}

export interface Rows {
  columns: string[]
  rows: unknown[][]
}

export interface Chart {
  x: string[]
  series: { name: string; values: number[] }[]
}

export interface TraceItem {
  tool: string
  args: Record<string, unknown> | null
  allowed: boolean
  code?: string
  summary: string
}

export interface Approval {
  token: string
  sql?: string
  estimate: { bytes: number; usd: number; needs_approval: boolean; source: string }
}

export interface Answer {
  answer: string
  trace: TraceItem[]
  sql?: string
  table?: Rows
  chart?: Chart
  approval?: Approval
  warnings: string[]
}

export interface Metric {
  name: string
  description: string
  unit: string
  dims?: string[]
  sensitive: boolean
}
