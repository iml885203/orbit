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
})
