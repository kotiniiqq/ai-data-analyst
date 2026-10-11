import type { Answer } from '../types'
import { LineChart } from './LineChart'
import { DataTable } from './DataTable'
import { Trace } from './Trace'
import { ApprovalCard } from './ApprovalCard'

const WARNINGS: Record<string, string> = {
  number_not_grounded: 'The drafted answer used numbers the data did not contain, so you see the data itself.',
  step_limit: 'The question needed more steps than allowed; try a narrower question.',
  planner_error: 'The assistant could not finish this answer.',
}

type Props = { answer: Answer; onApprove: (token: string) => void; busy: boolean }

export function AnswerCard({ answer, onApprove, busy }: Props) {
  return (
    <article className="answer">
      {!answer.approval && <p className="answer-text">{answer.answer}</p>}
      {answer.warnings.map((w) => (
        <p key={w} className="warning" role="note">
          {WARNINGS[w] ?? w}
        </p>
      ))}
      {answer.approval && <ApprovalCard approval={answer.approval} onApprove={onApprove} busy={busy} />}
      {answer.chart && <LineChart chart={answer.chart} />}
      {answer.table && answer.table.rows.length > 0 && <DataTable rows={answer.table} />}
      {answer.sql && !answer.approval && (
        <details className="sql-block">
          <summary>SQL</summary>
          <pre className="sql">{answer.sql}</pre>
        </details>
      )}
      <Trace items={answer.trace} />
    </article>
  )
}
