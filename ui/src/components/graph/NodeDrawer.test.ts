import { beforeEach, describe, it, expect, vi } from 'vitest'
import { render, fireEvent, waitFor } from '@testing-library/svelte'
import NodeDrawer from './NodeDrawer.svelte'
import { store } from '../../lib/stores.svelte'

describe('NodeDrawer', () => {
  beforeEach(() => {
    store.graph.data = null
    store.graph.preview = null
    store.daemon.envs = null
    vi.restoreAllMocks()
    vi.spyOn(globalThis, 'fetch').mockImplementation(async () =>
      new Response(JSON.stringify({ ok: true, data: {} }), { status: 200 }),
    )
  })

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
    expect(queryByText('Logs')).toBeNull()
  })

  it('explains that a health failure is live and self-recovers after the fix', () => {
    const node = {
      name: 'api',
      kind: 'backend' as const,
      state: 'degraded',
      stateReason: 'HTTP 500 from http://localhost:3000/health',
      failureKind: 'health',
      logsAvailable: true,
    }
    const { getByRole, getByText } = render(NodeDrawer, {
      props: { node, onClose: () => {} },
    })

    expect(getByRole('status', { name: 'Health check failure' })).toBeTruthy()
    expect(getByText('The process is still running')).toBeTruthy()
    expect(getByText(/recover automatically after the health endpoint is fixed/)).toBeTruthy()
    expect(getByText(/restart only retries the process/)).toBeTruthy()
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

  it('starts the direct blocker instead of restarting the dependent', async () => {
    const node = {
      name: 'api',
      kind: 'backend' as const,
      state: 'degraded',
      stateReason: 'dependency redis is stopped',
      blockedBy: 'redis',
    }
    store.graph.data = {
      env: 'local',
      previewOnly: false,
      nodes: [
        node,
        { name: 'redis', kind: 'infra', state: 'stopped' },
      ],
      edges: [{ from: 'api', to: 'redis', kind: 'dependency', detachable: false, detached: false }],
    }
    const fetchMock = vi.mocked(globalThis.fetch)
    const { getByRole, queryByRole } = render(NodeDrawer, {
      props: { node, onClose: () => {} },
    })

    expect(queryByRole('button', { name: 'Restart' })).toBeNull()
    await fireEvent.click(getByRole('button', { name: 'Start redis' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/up', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ resources: ['redis'] }),
    }))
  })

  it('follows a blocked chain to the root dependency', () => {
    const node = {
      name: 'shop',
      kind: 'frontend' as const,
      state: 'degraded',
      stateReason: 'dependency api is unavailable',
      blockedBy: 'api',
    }
    store.graph.data = {
      env: 'local',
      previewOnly: false,
      nodes: [
        node,
        {
          name: 'api',
          kind: 'backend',
          state: 'degraded',
          stateReason: 'dependency redis is stopped',
          blockedBy: 'redis',
        },
        { name: 'redis', kind: 'infra', state: 'stopped' },
      ],
      edges: [
        { from: 'shop', to: 'api', kind: 'dependency', detachable: false, detached: false },
        { from: 'api', to: 'redis', kind: 'dependency', detachable: false, detached: false },
      ],
    }
    const { getByRole, queryByRole } = render(NodeDrawer, {
      props: { node, onClose: () => {} },
    })

    expect(getByRole('button', { name: 'Start redis' })).toBeTruthy()
    expect(queryByRole('button', { name: 'Restart' })).toBeNull()
  })

  it('restarts a live but unhealthy root dependency', async () => {
    const node = {
      name: 'shop',
      kind: 'frontend' as const,
      state: 'degraded',
      stateReason: 'dependency api is degraded',
      blockedBy: 'api',
    }
    store.graph.data = {
      env: 'local',
      previewOnly: false,
      nodes: [
        node,
        {
          name: 'api',
          kind: 'backend',
          state: 'degraded',
          stateReason: 'health check returned 503',
        },
      ],
      edges: [{ from: 'shop', to: 'api', kind: 'dependency', detachable: false, detached: false }],
    }
    const fetchMock = vi.mocked(globalThis.fetch)
    const { getByRole, queryByRole } = render(NodeDrawer, {
      props: { node, onClose: () => {} },
    })

    expect(queryByRole('button', { name: 'Start api' })).toBeNull()
    await fireEvent.click(getByRole('button', { name: 'Restart api' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/restart/api', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{}',
    }))
  })
})
