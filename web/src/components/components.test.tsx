import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LineChart } from './LineChart'
import { DataTable } from './DataTable'
import { Trace } from './Trace'
import { ApprovalCard } from './ApprovalCard'
import { AnswerCard } from './AnswerCard'
import { Login } from './Login'
import type { Answer } from '../types'

test('line chart draws one line per series and labels the ends', () => {
  const { container } = render(
    <LineChart chart={{ x: ['2026-07', '2026-08', '2026-09'], series: [{ name: 'team', values: [1, 2, 3] }, { name: 'starter', values: [3, 2, 1] }] }} />,
  )
  expect(container.querySelectorAll('path.line')).toHaveLength(2)
  expect(screen.getByText('Jul 2026')).toBeInTheDocument()
  expect(screen.getByText('Sep 2026')).toBeInTheDocument()
  expect(screen.getByRole('img', { name: /team, starter/ })).toBeInTheDocument()
})

test('data table shows 10 rows until expanded', async () => {
  const rows = Array.from({ length: 25 }, (_, i) => [`d${i}`, i * 1000.5])
  render(<DataTable rows={{ columns: ['day', 'value'], rows }} />)
  expect(screen.getAllByRole('row')).toHaveLength(11)
  expect(screen.getByText('1,000.5')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Show all 25 rows' }))
  expect(screen.getAllByRole('row')).toHaveLength(26)
})

test('trace stamps denied and held calls', () => {
  render(
    <Trace
      items={[
        { tool: 'metric_series', args: { metric: 'mrr' }, allowed: true, summary: 'mrr ...' },
        { tool: 'run_sql', args: { sql: '(hidden)' }, allowed: false, code: 'forbidden', summary: 'your role (viewer) may not use run_sql' },
        { tool: 'describe_metric', args: { metric: 'x' }, allowed: true, code: 'unknown_metric', summary: 'no metric' },
      ]}
    />,
  )
  const items = screen.getAllByRole('listitem')
  expect(within(items[0]).getByText('allowed')).toBeInTheDocument()
  expect(within(items[1]).getByText('denied')).toBeInTheDocument()
  expect(within(items[2]).getByText('failed')).toBeInTheDocument()
})

test('approval card shows the estimate and runs on click', async () => {
  const onApprove = vi.fn()
  render(
    <ApprovalCard
      approval={{ token: 't', sql: 'select 1', estimate: { bytes: 300 * 1024 * 1024, usd: 0.0018, needs_approval: true, source: 'postgres explain' } }}
      onApprove={onApprove}
      busy={false}
    />,
  )
  expect(screen.getByText(/300 MiB/)).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Run this query' }))
  expect(onApprove).toHaveBeenCalledWith('t')
})

const answer: Answer = {
  answer: 'MRR in Sep 2026 was $284,830.27.',
  trace: [],
  table: { columns: ['month', 'mrr'], rows: [['2026-09', 284830.27]] },
  warnings: [],
}

test('answer card hides the SQL block when the server sent none', () => {
  render(<AnswerCard answer={answer} onApprove={() => {}} busy={false} />)
  expect(screen.getByText('MRR in Sep 2026 was $284,830.27.')).toBeInTheDocument()
  expect(screen.queryByText('SQL')).not.toBeInTheDocument()
})

test('answer card shows SQL and explains warnings', () => {
  render(<AnswerCard answer={{ ...answer, sql: 'SELECT 1', warnings: ['number_not_grounded'] }} onApprove={() => {}} busy={false} />)
  expect(screen.getByText('SELECT 1')).toBeInTheDocument()
  expect(screen.getByText(/numbers the data did not contain/)).toBeInTheDocument()
})

test('login offers the three demo users', async () => {
  const onLogin = vi.fn().mockResolvedValue(undefined)
  render(<Login onLogin={onLogin} />)
  await userEvent.click(screen.getByRole('button', { name: /Analyst/ }))
  expect(onLogin).toHaveBeenCalledWith('analyst@demo.local', 'analyst-demo')
})

test('login shows the error it gets', async () => {
  const onLogin = vi.fn().mockRejectedValue(new Error('wrong email or password'))
  render(<Login onLogin={onLogin} />)
  await userEvent.type(screen.getByLabelText('Email'), 'x@demo.local')
  await userEvent.type(screen.getByLabelText('Password'), 'nope')
  await userEvent.click(screen.getByRole('button', { name: 'Sign in' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('wrong email or password')
})
