import type { TraceItem } from '../types'

function verdict(t: TraceItem): 'allowed' | 'denied' | 'failed' {
  if (!t.allowed) return 'denied'
  return t.code ? 'failed' : 'allowed'
}

function argsText(args: TraceItem['args']): string {
  if (!args) return ''
  return Object.entries(args)
    .map(([k, v]) => `${k}=${typeof v === 'string' ? v : JSON.stringify(v)}`)
    .join('  ')
}

/** The receipt: every tool call behind an answer, with its permission verdict. */
export function Trace({ items }: { items: TraceItem[] }) {
  if (items.length === 0) return null
  return (
    <details className="trace">
      <summary>
        How I got this · {items.length} tool {items.length === 1 ? 'call' : 'calls'}
      </summary>
      <ol>
        {items.map((t, i) => {
          const v = verdict(t)
          return (
            <li key={i}>
              <span className={`stamp stamp-${v}`}>{v}</span>
              <code className="tool">{t.tool}</code>
              <code className="args">{argsText(t.args)}</code>
              <p>{t.summary}</p>
            </li>
          )
        })}
      </ol>
    </details>
  )
}
