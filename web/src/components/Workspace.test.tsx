import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Workspace } from './Workspace'
import type { Api } from '../api'
import type { User } from '../types'

const analyst: User = { id: 2, email: 'analyst@demo.local', role: 'analyst', perms: ['ai.chat', 'metrics.view', 'sql.run'] }

function fakeApi(overrides: Partial<Api> = {}): Api {
  return {
    login: vi.fn(),
    chat: vi.fn().mockResolvedValue({ answer: 'MRR in Sep 2026 was $284,830.27.', trace: [], warnings: [] }),
    approve: vi.fn(),
    catalog: vi.fn().mockResolvedValue({ metrics: [{ name: 'mrr', description: 'MRR', unit: 'USD', sensitive: false }] }),
    ...overrides,
  }
}

test('asking a question shows the question and the answer', async () => {
  const api = fakeApi()
  render(<Workspace api={api} user={analyst} onSignOut={() => {}} />)
  await userEvent.type(screen.getByLabelText('Your question'), 'What was MRR last month?')
  await userEvent.click(screen.getByRole('button', { name: 'Ask' }))
  expect(api.chat).toHaveBeenCalledWith('What was MRR last month?')
  expect(await screen.findByText('MRR in Sep 2026 was $284,830.27.')).toBeInTheDocument()
  expect(screen.getByText('What was MRR last month?')).toBeInTheDocument()
  expect(screen.getByLabelText('Your question')).toHaveValue('')
})

test('example questions follow the role', () => {
  render(<Workspace api={fakeApi()} user={analyst} onSignOut={() => {}} />)
  expect(screen.getByRole('button', { name: /select plan/i })).toBeInTheDocument()
  render(<Workspace api={fakeApi()} user={{ ...analyst, role: 'viewer' }} onSignOut={() => {}} />)
  expect(screen.getAllByRole('button', { name: /gross margin/i }).length).toBeGreaterThan(0)
})

test('approving a held query adds its result', async () => {
  const api = fakeApi({
    chat: vi.fn().mockResolvedValue({
      answer: 'This query would process about 300 MiB.',
      trace: [],
      warnings: [],
      approval: { token: 'tok', sql: 'select 1', estimate: { bytes: 300 << 20, usd: 0.002, needs_approval: true, source: 'x' } },
    }),
    approve: vi.fn().mockResolvedValue({ answer: 'The approved query returned 1000 rows.', trace: [], warnings: [] }),
  })
  render(<Workspace api={api} user={analyst} onSignOut={() => {}} />)
  await userEvent.type(screen.getByLabelText('Your question'), 'big one')
  await userEvent.click(screen.getByRole('button', { name: 'Ask' }))
  await userEvent.click(await screen.findByRole('button', { name: 'Run this query' }))
  expect(api.approve).toHaveBeenCalledWith('tok')
  expect(await screen.findByText('The approved query returned 1000 rows.')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Run this query' })).not.toBeInTheDocument()
})

test('a failed request is shown as an error on that question', async () => {
  const api = fakeApi({ chat: vi.fn().mockRejectedValue(new Error('the message must be 1 to 2000 characters')) })
  render(<Workspace api={api} user={analyst} onSignOut={() => {}} />)
  await userEvent.type(screen.getByLabelText('Your question'), 'x')
  await userEvent.click(screen.getByRole('button', { name: 'Ask' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('the message must be 1 to 2000 characters')
})
