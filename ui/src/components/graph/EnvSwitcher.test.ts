import { fireEvent, render, screen } from '@testing-library/svelte'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnvSwitcher from './EnvSwitcher.svelte'
import { store } from '$lib/stores.svelte'

vi.mock('$lib/api', () => ({
  apiPost: vi.fn(),
  fetchGraph: vi.fn(),
  switchEnv: vi.fn(),
}))

const stoppedNode = { name: 'web', kind: 'frontend', state: 'stopped', url: 'http://localhost:3000' }
const liveGraph = { env: 'development', nodes: [stoppedNode], edges: [] }
const previewGraph = { env: 'example', nodes: [], edges: [] }

describe('EnvSwitcher', () => {
  beforeEach(() => {
    store.graph.data = liveGraph
    store.graph.preview = null
    store.graph.selectedNode = null
    store.daemon.services = {}
    store.daemon.logModal = { target: null, loading: false }
    store.daemon.envs = {
      running: 0,
      envs: [
        { name: 'development.yaml', path: '/envs/development.yaml', current: true },
        { name: 'example.yaml', path: '/envs/example.yaml', current: false },
      ],
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
})
