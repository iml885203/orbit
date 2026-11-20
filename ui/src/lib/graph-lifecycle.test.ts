import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { tick } from 'svelte'
import { mutationsDisabled, store } from './stores.svelte'
import type { GraphResponse, GraphRuntimeContext, StatusResponse } from './types.gen'
const { fetchGraph, detachEdge } = vi.hoisted(() => ({ fetchGraph: vi.fn(), detachEdge: vi.fn() }))
vi.mock('./api', () => ({ fetchGraph, detachEdge }))
const owner: GraphRuntimeContext = { epoch: 101, generation: 1, config_path: '/workspace/local/orbit.yaml' }
const graph = (runtime: GraphRuntimeContext = owner): GraphResponse => ({ env: 'local', runtime, nodes: [], edges: [] })
const status = (runtime: GraphRuntimeContext = owner, identity = 'source/local'): StatusResponse => ({
  ...runtime, resources: [], context: { kind: 'managed', identity, display_name: 'local',
    config_path: runtime.config_path, available: true, running: false },
})
function observe(runtime: GraphRuntimeContext = owner, identity = 'source/local') {
  store.daemon.runtimeContext = runtime
  store.daemon.environmentIdentity = identity
  store.daemon.connected = true
  store.graph.observeStatus(status(runtime, identity))
}
const settle = async () => { await Promise.resolve(); await tick(); await Promise.resolve() }
beforeEach(() => {
  vi.useFakeTimers()
  store.graph.dispose()
  vi.resetAllMocks()
  store.graph.data = null
  store.daemon.runtimeContext = null
  store.daemon.environmentIdentity = ''
  store.daemon.connected = false
  fetchGraph.mockImplementation(async () => graph(store.daemon.runtimeContext ?? owner))
})
afterEach(() => { store.graph.dispose(); vi.useRealTimers() })

describe('graph authority lifecycle', () => {
  it('renders bootstrap read-only, confirms status, and revokes controls on disconnect', async () => {
    store.graph.refresh()
    await settle()
    expect(store.graph.data?.env).toBe('local')
    expect(mutationsDisabled()).toBe(true)
    observe()
    await settle()
    expect(mutationsDisabled()).toBe(false)
    const snapshot = store.graph.data
    store.daemon.connected = false
    store.graph.disconnect()
    expect(store.graph.data).toBe(snapshot)
    expect(mutationsDisabled()).toBe(true)
    observe()
    await settle()
    expect(mutationsDisabled()).toBe(false)
  })

  it('starts B immediately despite a hung A read and rejects late A', async () => {
    let releaseA!: (value: GraphResponse) => void
    fetchGraph.mockReturnValueOnce(new Promise(resolve => { releaseA = resolve }))
    observe()
    const replacement = { ...owner, generation: 2, config_path: '/workspace/b/orbit.yaml' }
    observe(replacement, 'source/b')
    expect(fetchGraph).toHaveBeenCalledTimes(2)
    await settle()
    releaseA(graph())
    await settle()
    expect(store.graph.data?.runtime).toEqual(replacement)
  })

  it('requires a replacement owner for same-identity switch, retains stalled state, then recovers', async () => {
    observe()
    await settle()
    store.graph.beginSwitch('source/local', 0)
    store.graph.receiveSwitch(true, { ok: true })
    observe()
    await settle()
    expect(store.graph.envSwitching?.phase).toBe('loading')
    expect(mutationsDisabled()).toBe(true)
    await vi.advanceTimersByTimeAsync(30_000)
    expect(store.graph.envSwitching?.phase).toBe('stalled')
    expect(mutationsDisabled()).toBe(true)
    observe({ ...owner, epoch: 202 })
    await settle()
    expect(store.graph.envSwitching).toBeNull()
    expect(mutationsDisabled()).toBe(false)
  })

  it('rejects overlapping switches and keeps a late acknowledgement stalled', async () => {
    observe()
    await settle()
    expect(store.graph.beginSwitch('source/target', 0)).toBe(true)
    expect(store.graph.beginSwitch('source/other', 0)).toBe(false)
    await vi.advanceTimersByTimeAsync(30_000)
    store.graph.receiveSwitch(true)
    expect(store.graph.envSwitching).toEqual({ target: 'source/target', total: 0, phase: 'stalled' })
    expect(mutationsDisabled()).toBe(true)
  })

  it('does not confuse a new owner with the same display name but a different identity', async () => {
    observe()
    await settle()
    store.graph.beginSwitch('source/target', 0)
    store.graph.receiveSwitch(true)
    observe({ ...owner, epoch: 202 }, 'source/other')
    await settle()
    expect(store.graph.envSwitching).not.toBeNull()
    observe({ ...owner, epoch: 202 }, 'source/target')
    await settle()
    expect(store.graph.envSwitching).toBeNull()
  })

  it('returns rejected switches to confirmed live authority without discarding the graph', async () => {
    observe()
    await settle()
    const snapshot = store.graph.data
    store.graph.beginSwitch('source/target', 0)
    store.graph.receiveSwitch(false, { error: 'rejected' })
    expect(store.graph.data).toBe(snapshot)
    expect(mutationsDisabled()).toBe(true)
    await settle()
    expect(mutationsDisabled()).toBe(false)
  })

  it.each(['exit', 'switch', 'newer preview'])('rejects late preview after %s', async action => {
    let release!: (value: GraphResponse) => void
    fetchGraph.mockReturnValueOnce(new Promise(resolve => { release = resolve }))
    const pending = store.graph.requestPreview('source/old')
    if (action === 'exit') store.graph.exitPreview()
    else if (action === 'switch') store.graph.beginSwitch('source/target', 0)
    else {
      fetchGraph.mockResolvedValueOnce({ env: 'source/new', nodes: [], edges: [] })
      await store.graph.requestPreview('source/new')
    }
    release({ env: 'source/old', nodes: [], edges: [] })
    expect(await pending).toBe(false)
    expect(store.graph.preview?.env ?? null).toBe(action === 'newer preview' ? 'source/new' : null)
  })

  it('bounds mismatched-owner recovery without identical SSE resetting the retry budget', async () => {
    fetchGraph.mockResolvedValue(graph({ ...owner, generation: 0 }))
    observe()
    await settle()
    await vi.advanceTimersByTimeAsync(7_000)
    expect(fetchGraph).toHaveBeenCalledTimes(4)
    for (let i = 0; i < 10; i++) store.graph.observeStatus(status())
    await vi.advanceTimersByTimeAsync(30_000)
    expect(fetchGraph).toHaveBeenCalledTimes(4)
    expect(store.graph.freshness).toBe('unavailable')
    fetchGraph.mockResolvedValue(graph())
    store.graph.refresh()
    await settle()
    expect(store.graph.freshness).toBe('current')
  })

  it('bounds a hung current read and exposes refresh failure without losing confirmed graph', async () => {
    observe()
    await settle()
    const snapshot = store.graph.data
    fetchGraph.mockReturnValueOnce(new Promise(() => {}))
    store.graph.refresh()
    await vi.advanceTimersByTimeAsync(10_000)
    expect(store.graph.freshness).toBe('unavailable')
    expect(store.graph.data).toBe(snapshot)
    fetchGraph.mockResolvedValueOnce(graph())
    store.graph.refresh()
    await settle()
    expect(store.graph.freshness).toBe('current')
  })
})
