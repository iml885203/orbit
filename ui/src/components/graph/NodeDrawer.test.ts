import { describe, it, expect } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import NodeDrawer from './NodeDrawer.svelte'

describe('NodeDrawer', () => {
  it('renders nothing when no node selected', () => {
    const { queryByRole } = render(NodeDrawer, { props: { node: null, onClose: () => {} } })
    expect(queryByRole('dialog')).toBeNull()
  })

  it('renders the dialog when a node is selected', () => {
    const node = { name: 'frontend', kind: 'frontend' as const, state: 'healthy' }
    const { getByRole, getByText } = render(NodeDrawer, { props: { node, onClose: () => {} } })
    expect(getByRole('dialog')).toBeTruthy()
    expect(getByText('frontend')).toBeTruthy()
  })

  it('shows an infra icon container for infra nodes', () => {
    const node = { name: 'mongodb', kind: 'infra' as const, state: 'healthy' }
    const { getByTestId } = render(NodeDrawer, { props: { node, onClose: () => {} } })
    expect(getByTestId('node-drawer-infra-icon')).toBeTruthy()
  })

  it('does not show an infra icon container for frontend nodes', () => {
    const node = { name: 'frontend', kind: 'frontend' as const, state: 'healthy' }
    const { queryByTestId } = render(NodeDrawer, { props: { node, onClose: () => {} } })
    expect(queryByTestId('node-drawer-infra-icon')).toBeNull()
  })

  it('shows an infra icon container for infra nodes without node.icon so the Cog fallback can render', () => {
    const node = { name: 'clickhouse', kind: 'infra' as const, state: 'healthy' }
    const { getByTestId } = render(NodeDrawer, { props: { node, onClose: () => {} } })
    expect(getByTestId('node-drawer-infra-icon')).toBeTruthy()
  })

  it('calls onClose when Esc is pressed', async () => {
    let closed = false
    const node = { name: 'frontend', kind: 'frontend' as const, state: 'healthy' }
    render(NodeDrawer, { props: { node, onClose: () => { closed = true } } })
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(closed).toBe(true)
  })

  it('shows port recovery instead of a blind restart', () => {
    const node = {
      name: 'redis',
      kind: 'infra' as const,
      state: 'degraded',
      stateReason: 'cannot start redis: port 26379 is already in use',
      portConflict: {
        port: 26379,
        resource: 'redis',
        inspect_command: 'lsof -nP -iTCP:26379 -sTCP:LISTEN',
      },
    }
    const { getByRole, getByText, queryByText } = render(NodeDrawer, {
      props: { node, onClose: () => {} },
    })

    expect(getByRole('alert', { name: 'Port conflict' })).toBeTruthy()
    expect(getByText('Port 26379 is already in use')).toBeTruthy()
    expect(getByText('Copy inspection command')).toBeTruthy()
    expect(getByText('Port blocked')).toBeTruthy()
    expect(queryByText('Restart')).toBeNull()
  })

  it('names the dependency whose port must change', () => {
    const node = {
      name: 'api',
      kind: 'backend' as const,
      state: 'pending',
      portConflict: {
        port: 6379,
        resource: 'redis',
        inspect_command: 'lsof -nP -iTCP:6379 -sTCP:LISTEN',
      },
    }
    const { getByText } = render(NodeDrawer, { props: { node, onClose: () => {} } })

    expect(getByText(/change redis's host port/)).toBeTruthy()
  })
})
