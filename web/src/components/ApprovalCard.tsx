import type { Approval } from '../types'
import { formatBytes } from '../format'

type Props = { approval: Approval; onApprove: (token: string) => void; busy: boolean }

export function ApprovalCard({ approval, onApprove, busy }: Props) {
  const e = approval.estimate
  return (
    <section className="approval" aria-label="Query waiting for approval">
      <span className="stamp stamp-held">held</span>
      <p>
        This query would process about <strong>{formatBytes(e.bytes)}</strong> (≈ ${e.usd.toFixed(4)} at on-demand
        prices). It runs only if you confirm.
      </p>
      {approval.sql && <pre className="sql">{approval.sql}</pre>}
      <button type="button" onClick={() => onApprove(approval.token)} disabled={busy}>
        {busy ? 'Running…' : 'Run this query'}
      </button>
    </section>
  )
}
