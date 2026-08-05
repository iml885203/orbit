import { fireEvent, render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnvSwitcher from './EnvSwitcher.svelte'
import { store } from '$lib/stores.svelte'
import { switchEnv } from '$lib/api'

vi.mock('$lib/api', () => ({
  apiPost: vi.fn(),
  fetchGraph: vi.fn(),
  switchEnv: vi.fn(),
}))

const stoppedNode = { name: 'web', kind: 'frontend', state: 'stopped', url: 'http://localhost:3000' }
const liveGraph = { env: 'default/development', nodes: [stoppedNode], edges: [] }
const previewGraph = { env: 'default/example', nodes: [], edges: [] }

describe('EnvSwitcher', () => {
  beforeEach(() => {
    vi.mocked(switchEnv).mockResolvedValue({ ok: true, data: { ok: true } })
    store.graph.data = liveGraph
    store.graph.preview = null
    store.graph.selectedNode = null
    store.daemon.services = {}
    store.daemon.instanceName = ''
    store.daemon.logModal = { target: null, loading: false }
    store.daemon.envs = {
      running: 0,
      context: {
        kind: 'managed',
        identity: 'default/development',
        display_name: 'development',
        config_path: '/envs/development.yaml',
        available: true,
        running: false,
        managed_selection: { identity: 'default/development', name: 'development', path: '/envs/development.yaml', active: true },
      },
      sources: [{ name: 'default', type: 'git', location: 'https://example.com/envs.git', environments: [
        { identity: 'default/development', name: 'development', path: '/envs/development.yaml', selected: true, running: false },
        { identity: 'default/example', name: 'example', path: '/envs/example.yaml', selected: false, running: false },
      ] }],
    }
  })

  it('gives a stopped environment one primary startup path', () => {
    render(EnvSwitcher)

    expect(screen.getByText('development')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Start environment' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Stop environment' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Infra Only' })).not.toBeInTheDocument()
    expect(screen.queryByRole('tab')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'example' })).not.toBeInTheDocument()
  })

  it('uses project context in the graph label and switch confirmation', async () => {
    if (!store.daemon.envs) throw new Error('env fixture missing')
    store.daemon.envs.context = {
      kind: 'project',
      identity: '/work/payments/orbit.yaml',
      display_name: 'payments',
      config_path: '/work/payments/orbit.yaml',
      project_root: '/work/payments',
      available: true,
      running: true,
      managed_selection: { identity: 'default/development', name: 'development', path: '/envs/development.yaml', active: false },
    }
    store.daemon.services = {
      web: {
        name: 'web',
        kind: 'service',
        state: 'healthy',
        restart_count: 0,
        external_restart_count: 0,
      },
    }
    vi.mocked(switchEnv).mockResolvedValue({
      ok: false,
      data: {
        error: 'confirmation required',
        confirmation_required: true,
        current_context: store.daemon.envs.context,
        target_context: {
          kind: 'managed',
          identity: 'default/example',
          display_name: 'example',
          config_path: '/envs/example.yaml',
          available: true,
          running: false,
        },
        running_resources: ['web'],
      },
    })

    render(EnvSwitcher)
    expect(screen.getByText('payments')).toBeInTheDocument()
    expect(screen.getByText('Project environment')).toBeInTheDocument()
    store.graph.preview = previewGraph
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Use this env' }))
    expect(switchEnv).toHaveBeenCalledWith('default/example', false, '', '', [])
    expect(screen.getByText(/stop 1 running item from payments/)).toBeInTheDocument()
	await fireEvent.click(screen.getByRole('button', { name: 'Stop and switch' }))
	expect(switchEnv).toHaveBeenLastCalledWith(
	  'default/example',
	  true,
	  '/work/payments/orbit.yaml',
	  'default/example',
	  ['web'],
	)
  })

  it('leads a healthy environment with its application instead of lifecycle controls', () => {
    store.graph.data = {
      ...liveGraph,
      nodes: [{ ...stoppedNode, name: 'storefront', state: 'healthy' }],
    }
    store.daemon.services = {
      storefront: {
        name: 'storefront',
        kind: 'service',
        state: 'healthy',
        restart_count: 0,
        external_restart_count: 0,
      },
    }
    render(EnvSwitcher)

    expect(screen.getByRole('button', { name: 'Open storefront' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Stop environment' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Start environment' })).not.toBeInTheDocument()
    expect(screen.getByText('1 healthy')).toBeInTheDocument()
  })

  it('leads a blocked graph to the root resource logs', async () => {
    store.graph.data = {
      ...liveGraph,
      nodes: [
        { ...stoppedNode, name: 'web', state: 'degraded', blockedBy: 'api' },
        { name: 'api', kind: 'backend', state: 'degraded', blockedBy: 'redis' },
        { name: 'redis', kind: 'infra', state: 'degraded', logsAvailable: true },
      ],
    }
    render(EnvSwitcher)

    const action = screen.getByRole('button', { name: 'View redis logs' })
    await fireEvent.click(action)
    expect(store.daemon.logModal.target).toBe('redis')
  })

  it('opens health details instead of treating a live process like a crash', async () => {
    store.graph.data = {
      ...liveGraph,
      nodes: [{
        ...stoppedNode,
        name: 'api',
        kind: 'backend',
        state: 'degraded',
        stateReason: 'HTTP 500 from http://localhost:3000/health',
        failureKind: 'health',
        logsAvailable: true,
      }],
    }
    render(EnvSwitcher)

    await fireEvent.click(screen.getByRole('button', { name: 'Inspect api health' }))
    expect(store.graph.selectedNode).toBe('api')
    expect(store.daemon.logModal.target).toBeNull()
  })

  it('shows explicit activation controls only while previewing', () => {
    store.graph.preview = previewGraph
    render(EnvSwitcher)

    expect(screen.getByText('Previewing')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Use this env' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Exit preview' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Start environment' })).not.toBeInTheDocument()
  })

  it('names the runtime in a high-impact confirmation', async () => {
    store.daemon.instanceName = 'checkout-a'
    store.daemon.services = {
      api: {
        name: 'api',
        kind: 'service',
        state: 'healthy',
        restart_count: 0,
        external_restart_count: 0,
      },
    }
    render(EnvSwitcher)

    await fireEvent.click(screen.getByRole('button', { name: 'Stop environment' }))

    expect(screen.getByRole('dialog')).toHaveTextContent('instance checkout-a')
  })
})
