import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'
import NodeDrawerSelection from './NodeDrawerSelection.test.svelte'
import { replaceServices, resetForNewDaemon, store } from '$lib/stores.svelte'
import type { GraphNode, GraphResponse, GraphRuntimeContext, ResourceStatus } from '$lib/types.gen'

const owner: GraphRuntimeContext = { epoch: 101, generation: 2, config_path: '/workspace/local/orbit.yaml' }
function graph(node: Partial<GraphNode> = {}, runtime: GraphRuntimeContext | undefined = owner): GraphResponse {
  return { env: 'local', runtime, nodes: [{ name: 'api', kind: 'backend', runtime_kind: 'service', state: 'healthy',
    restart_count: 1, startup_time: '1s', uptime: '10s', ...node }], edges: [] }
}
function status(resource: Partial<ResourceStatus> = {}): ResourceStatus {
  return { name: 'api', kind: 'service', state: 'healthy', restart_count: 3,
    external_restart_count: 0, startup_time: '2s', uptime: '30s', ...resource }
}
function expectSnapshot() {
  expect(screen.getByText('restarts: 1')).toBeInTheDocument()
  expect(screen.getByText('started in 1s')).toBeInTheDocument()
  expect(screen.getByText('up 10s')).toBeInTheDocument()
  expect(screen.queryByText('restarts: 3')).not.toBeInTheDocument()
}
beforeEach(() => {
  resetForNewDaemon()
  store.graph.preview = null
  store.graph.envSwitching = null
  store.graph.selectedNode = 'api'
  store.graph.data = graph()
  store.daemon.daemonEpoch = owner.epoch
  store.daemon.connected = true
  store.daemon.runtimeContext = owner
  store.graph.publish(store.graph.data!)
  vi.spyOn(globalThis, 'fetch').mockImplementation(async () => new Response(JSON.stringify({ env: [] })))
})
afterEach(() => {
  cleanup()
  resetForNewDaemon()
  store.graph.data = null
  store.graph.preview = null
  store.graph.selectedNode = null
  store.graph.envSwitching = null
  store.daemon.daemonEpoch = null
  store.daemon.connected = false
  vi.restoreAllMocks()
})

describe('selected live drawer runtime', () => {
  it('updates restart count, startup duration and uptime without replacing topology', async () => {
    render(NodeDrawerSelection)
    expectSnapshot()
    const initialGraph = store.graph.data
    replaceServices([status()], owner)
    store.graph.publish(graph())
    await tick()
    expect(screen.getByText('restarts: 3')).toBeInTheDocument()
    expect(screen.getByText('started in 2s')).toBeInTheDocument()
    expect(screen.getByText('up 30s')).toBeInTheDocument()
    expect(store.graph.data).toBe(initialGraph)
    expect(store.graph.selectedNode).toBe('api')
  })

  it.each([
    ['another project with the same display name', { ...owner, config_path: '/another/local/orbit.yaml' }],
    ['a previous daemon', { ...owner, epoch: 100 }],
    ['an earlier environment generation', { ...owner, generation: 1 }],
  ])('rejects status from %s', async (_name, previous) => {
    render(NodeDrawerSelection)
    replaceServices([status()], previous)
    await tick()
    expectSnapshot()
  })

  it('keeps a new context status out of a late previous-context graph response', async () => {
    const current = { ...owner, generation: 3, config_path: '/another/local/orbit.yaml' }
    store.graph.data = graph({}, current)
    replaceServices([status()], current)
    render(NodeDrawerSelection)
    expect(screen.getByText('restarts: 3')).toBeInTheDocument()
    const initialGraph = store.graph.data
    store.graph.publish(graph())
    await tick()
    expect(screen.getByText('restarts: 3')).toBeInTheDocument()
    expect(store.graph.data).toBe(initialGraph)
    store.graph.publish(graph({}, current))
    await tick()
    expect(screen.getByText('restarts: 3')).toBeInTheDocument()
    expect(store.graph.data).toBe(initialGraph)
  })

  it('does not borrow same-name runtime while switching or disconnected', async () => {
    render(NodeDrawerSelection)
    replaceServices([status()], owner)
    store.graph.envSwitching = { target: 'other', total: 1, phase: 'loading' }
    await tick()
    expectSnapshot()
    store.graph.envSwitching = null
    store.daemon.connected = false
    await tick()
    expectSnapshot()
  })

  it('invalidates live runtime when the daemon resets', async () => {
    render(NodeDrawerSelection)
    replaceServices([status()], owner)
    await tick()
    expect(screen.getByText('restarts: 3')).toBeInTheDocument()
    resetForNewDaemon()
    store.daemon.daemonEpoch = 102
    replaceServices([status()], { ...owner, epoch: 102 })
    await tick()
    expectSnapshot()
  })

  it('keeps preview snapshots read-only and resumes live values on returning', async () => {
    store.graph.data = graph({ mode: 'dev' })
    store.graph.preview = { ...graph({ mode: 'dev', restart_count: 7, startup_time: '7s', uptime: '70s' }), env: 'other' }
    render(NodeDrawerSelection)
    replaceServices([status({ mode: 'dev' })], owner)
    await tick()
    expect(screen.getByText('restarts: 7')).toBeInTheDocument()
    expect(screen.getByText('started in 7s')).toBeInTheDocument()
    expect(screen.getByText('up 70s')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Dev' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Container' })).toBeDisabled()
    store.graph.preview = null
    await tick()
    expect(screen.getByText('restarts: 3')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Dev' })).toBeEnabled()
    store.graph.preview = { ...graph({ mode: 'dev', restart_count: 7 }), env: 'other' }
    await tick()
    expect(screen.getByText('restarts: 7')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Dev' })).toBeDisabled()
  })

  it.each([
    ['backend host process', 'backend', 'service', undefined],
    ['frontend host process', 'frontend', 'service', undefined],
    ['backend container', 'backend', 'container', undefined],
    ['dual resource in dev mode', 'backend', 'service', 'dev'],
    ['dual resource in container mode', 'backend', 'container', 'container'],
  ])('updates matching %s without confusing display and runtime kinds', async (_name, kind, runtimeKind, mode) => {
    store.graph.data = graph({ kind, runtime_kind: runtimeKind, mode })
    render(NodeDrawerSelection)
    replaceServices([status({ kind: runtimeKind, mode })], owner)
    await tick()
    expect(screen.getByText('restarts: 3')).toBeInTheDocument()
    if (mode) {
      expect(screen.getByRole('button', { name: mode === 'dev' ? 'Dev' : 'Container' })).toHaveClass('active')
    }
  })

  it('preserves the infrastructure drawer without adding service runtime controls', async () => {
    store.graph.data = graph({ kind: 'infra', runtime_kind: 'container' })
    render(NodeDrawerSelection)
    replaceServices([status({ kind: 'container' })], owner)
    await tick()
    expect(screen.getByTestId('node-drawer-infra-icon')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Runtime' })).not.toBeInTheDocument()
    expect(screen.queryByText(/^restarts:/)).not.toBeInTheDocument()
  })

  it.each([
    ['wrong name', { name: 'worker' }],
    ['wrong runtime kind', { kind: 'container' }],
    ['wrong active mode', { mode: 'container' }],
  ])('does not bind %s', async (_name, resource) => {
    render(NodeDrawerSelection)
    replaceServices([status(resource)], owner)
    await tick()
    expectSnapshot()
  })

  it('keeps graph snapshots when the live response has no runtime identity', async () => {
    store.graph.data = { ...graph(), runtime: undefined }
    render(NodeDrawerSelection)
    replaceServices([status()], owner)
    await tick()
    expectSnapshot()
  })

  it('clears removed timing and restart metadata instead of reviving the graph snapshot', async () => {
    render(NodeDrawerSelection)
    replaceServices([status({ restart_count: 0, startup_time: undefined, uptime: undefined })], owner)
    await tick()
    expect(screen.queryByText(/^restarts:/)).not.toBeInTheDocument()
    expect(screen.queryByText(/^started in/)).not.toBeInTheDocument()
    expect(screen.queryByText(/^up /)).not.toBeInTheDocument()
  })
})
