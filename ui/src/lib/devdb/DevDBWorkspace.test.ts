import { fireEvent, render, screen, waitFor, within } from '@testing-library/svelte'
import { describe, expect, it, vi } from 'vitest'
import DevDBWorkspace from './DevDBWorkspace.svelte'
import type { DBOpInFlight } from './stores.svelte'

vi.mock('gsap', () => ({ gsap: { to: vi.fn() } }))

const projects = [
  { name: 'Orders', path: '/repo/Orders.sqlproj', databases: ['OrdersDB'] },
  { name: 'Billing', path: '/repo/Billing.sqlproj', databases: ['BillingDB'] },
  { name: 'Empty', path: '/repo/Empty.sqlproj', databases: [] },
]
const props = { projects, states: {}, operation: null as DBOpInFlight | null, sqlServerHealthy: true, elapsedSeconds: 0, onDiff: vi.fn() }

describe('database workspace', () => {
  it('selects projects and scopes publish progress, success and failure to their databases', async () => {
    const view = render(DevDBWorkspace, { props })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Select Orders' })).toHaveAttribute('aria-pressed', 'true'))
    expect(screen.getByRole('region', { name: 'Orders databases' })).toBeInTheDocument()
    expect(screen.getByText('3 SQL projects')).toBeInTheDocument()
    const operation: DBOpInFlight = { op: 'publish', db: 'OrdersDB', startedAt: new Date().toISOString(), lines: [], done: false, ok: false }
    await view.rerender({ ...props, operation, elapsedSeconds: 8, disabledReason: 'DB operation in progress' })
    const orders = screen.getByRole('region', { name: 'Orders databases' })
    expect(within(orders).getByRole('status')).toHaveTextContent('Publishing')
    expect(within(orders).getByRole('button', { name: 'Publish' })).toBeDisabled()
    expect(screen.getByRole('img', { name: /visualization for Orders/ })).toHaveClass('building')

    await fireEvent.click(screen.getByRole('button', { name: 'Select Billing' }))
    expect(screen.getByRole('button', { name: 'Select Billing' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('img', { name: /visualization for Billing/ })).toHaveClass('ready')
    expect(within(screen.getByRole('region', { name: 'Billing databases' })).getByRole('status')).not.toHaveTextContent('Publishing')
    await fireEvent.click(screen.getByRole('button', { name: 'Select Orders' }))
    await view.rerender({ ...props, operation: { ...operation, done: true, ok: true }, states: { OrdersDB: { name: 'OrdersDB', lastPublish: { at: new Date().toISOString(), source: 'cli' } } } })
    expect(screen.getByRole('img', { name: /visualization for Orders/ })).toHaveClass('complete')
    expect(within(screen.getByRole('region', { name: 'Orders databases' })).getByRole('status')).toHaveTextContent('Published')
    await view.rerender({ ...props, operation: { ...operation, done: true, err: 'Publish rejected' } })
    expect(screen.getByRole('img', { name: /visualization for Orders/ })).toHaveClass('failed')
    expect(screen.getByRole('alert')).toHaveTextContent('Publish rejected')
    await fireEvent.click(screen.getByRole('button', { name: 'Select Empty' }))
    expect(screen.getByText('This SQL project does not declare any databases.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Publish' })).not.toBeInTheDocument()
  })

  it('shows an empty workspace and a single selected project without a running database', async () => {
    const view = render(DevDBWorkspace, { props: { ...props, projects: [] } })
    expect(screen.getByText('No DB projects found.')).toBeInTheDocument()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    await view.rerender({ ...props, projects: [projects[0]], sqlServerHealthy: false })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Select Orders' })).toHaveAttribute('aria-pressed', 'true'))
    expect(screen.getByText('1 SQL project')).toBeInTheDocument()
    expect(screen.getByRole('img', { name: /visualization for Orders/ })).toHaveClass('idle')
  })
})
