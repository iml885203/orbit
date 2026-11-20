import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { optimisticDetach } from './graphActions'
import { store } from './stores.svelte'
import type { GraphResponse } from './types.gen'
const { detachEdge, fetchGraph } = vi.hoisted(() => ({ detachEdge: vi.fn(), fetchGraph: vi.fn() }))
vi.mock('./api', () => ({ detachEdge, fetchGraph }))
const owner = { epoch: 101, generation: 1, config_path: '/workspace/local/orbit.yaml' }
function graph(first = false, second = false): GraphResponse {
  return { env: 'local', runtime: owner,
    nodes: [{ name: 'web', kind: 'frontend', state: 'healthy' },
      { name: 'api', kind: 'backend', state: 'healthy' },
      { name: 'db', kind: 'infra', state: 'healthy' }],
    edges: [{ from: 'web', to: 'api', kind: 'dependency', detachable: true, detached: first },
      { from: 'web', to: 'db', kind: 'dependency', detachable: true, detached: second }] }
}
function pendingAck() {
  let resolve!: (result: unknown) => void
  const promise = new Promise(done => { resolve = done })
  return { promise, resolve }
}
beforeEach(async () => {
  vi.resetAllMocks()
  fetchGraph.mockResolvedValue(null)
  store.graph.dispose()
  store.graph.data = graph()
  store.graph.preview = null
  store.graph.envSwitching = null
  store.daemon.daemonEpoch = owner.epoch
  store.daemon.runtimeContext = owner
  store.daemon.connected = true
  store.daemon.environmentIdentity = 'local'
  fetchGraph.mockResolvedValue(graph())
  store.graph.observeStatus({ ...owner, resources: [], context: { kind: 'project', identity: 'local',
    display_name: 'local', config_path: owner.config_path, available: true, running: false } })
  await vi.waitFor(() => expect(store.graph.canMutate).toBe(true))
  fetchGraph.mockResolvedValue(null)
})
afterEach(() => {
  store.graph.data = null
  store.daemon.runtimeContext = null
  store.daemon.connected = false
  store.daemon.daemonEpoch = null
})

describe('graph read and mutation interleaving', () => {
  it('does not let a pre-mutation graph read erase an optimistic detach', async () => {
    const ack = pendingAck()
    detachEdge.mockReturnValueOnce(ack.promise)
    const priorRead = graph()
    const operation = optimisticDetach(store.graph.data!.edges[0], true)
    await Promise.resolve()
    expect(store.graph.data!.edges[0].detached).toBe(true)
    store.graph.publish(priorRead)
    const whilePending = store.graph.data!.edges[0].detached
    ack.resolve({ ok: true, data: { graph: graph(true), message: 'detached' } })
    await operation
    expect(whilePending).toBe(true)
    expect(store.graph.data!.edges[0].detached).toBe(true)
  })

  it('keeps another edge optimistic when the first ack snapshot completes', async () => {
    const first = pendingAck()
    const second = pendingAck()
    detachEdge.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const firstOperation = optimisticDetach(store.graph.data!.edges[0], true)
    const secondOperation = optimisticDetach(store.graph.data!.edges[1], true)
    await Promise.resolve()
    await vi.waitFor(() => expect(detachEdge).toHaveBeenCalledTimes(2))
    first.resolve({ ok: true, data: { graph: graph(true, false), message: 'first detached' } })
    await firstOperation
    const secondWhilePending = store.graph.data!.edges[1].detached
    second.resolve({ ok: true, data: { graph: graph(true, true), message: 'second detached' } })
    await secondOperation
    expect(secondWhilePending).toBe(true)
    expect(store.graph.data!.edges.map(edge => edge.detached)).toEqual([true, true])
  })
  it('keeps successful writes with a missing returned edge as reconciliation unavailable', async () => {
    detachEdge.mockResolvedValueOnce({ ok: true, data: { graph: { ...graph(), edges: [] } } })
    expect(await optimisticDetach(store.graph.data!.edges[0], true)).toBe(true)
    expect(store.graph.data!.edges).toHaveLength(2)
    expect(store.ui.toastMessage).toContain('reconciliation unavailable')
  })

  it('retires an old-owner queue so a hung A write cannot block B', async () => {
    const old = pendingAck()
    const current = pendingAck()
    detachEdge.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const operationA = optimisticDetach(store.graph.data!.edges[0], true)
    await vi.waitFor(() => expect(detachEdge).toHaveBeenCalledTimes(1))
    const nextOwner = { ...owner, generation: 2, config_path: '/workspace/b/orbit.yaml' }
    store.daemon.runtimeContext = nextOwner
    store.daemon.environmentIdentity = 'b'
    const nextGraph = { ...graph(), env: 'b', runtime: nextOwner }
    fetchGraph.mockResolvedValue(nextGraph)
    store.graph.observeStatus({ ...nextOwner, resources: [], context: { kind: 'project', identity: 'b',
      display_name: 'local', config_path: nextOwner.config_path, available: true, running: false } })
    await vi.waitFor(() => expect(store.graph.data?.runtime).toEqual(nextOwner))
    const operationB = optimisticDetach(store.graph.data!.edges[0], false)
    await vi.waitFor(() => expect(detachEdge).toHaveBeenCalledTimes(2))
    current.resolve({ ok: true, data: { graph: nextGraph } })
    await operationB
    old.resolve({ ok: false, data: { error: 'late A failure' } })
    await operationA
    expect(store.graph.data?.runtime).toEqual(nextOwner)
    expect(store.graph.data?.edges[0].detached).toBe(false)
  })

  it('does not dispatch a newer write until an indeterminate same-owner write and reconnect read settle', async () => {
    const old = pendingAck()
    detachEdge.mockReturnValueOnce(old.promise)
    const operation = optimisticDetach(store.graph.data!.edges[0], true)
    await vi.waitFor(() => expect(detachEdge).toHaveBeenCalledTimes(1))
    store.daemon.connected = false
    store.graph.disconnect()
    expect(store.graph.data?.edges[0].detached).toBe(false)
    store.daemon.connected = true
    store.graph.observeStatus({ ...owner, resources: [], context: { kind: 'project', identity: 'local',
      display_name: 'local', config_path: owner.config_path, available: true, running: false } })
    expect(await optimisticDetach(store.graph.data!.edges[0], false)).toBe(false)
    expect(detachEdge).toHaveBeenCalledTimes(1)
    fetchGraph.mockResolvedValue(graph(true))
    old.resolve({ ok: true, data: { graph: graph(true) } })
    await operation
    await vi.waitFor(() => expect(store.graph.canMutate).toBe(true))
    detachEdge.mockResolvedValueOnce({ ok: true, data: { graph: graph(false) } })
    fetchGraph.mockResolvedValue(graph(false))
    expect(await optimisticDetach(store.graph.data!.edges[0], false)).toBe(true)
    expect(detachEdge).toHaveBeenCalledTimes(2)
    expect(store.graph.data?.edges[0].detached).toBe(false)
  })

  it('rolls back a failed old write and reconciles after a rejected switch', async () => {
    const ack = pendingAck()
    detachEdge.mockReturnValueOnce(ack.promise)
    const operation = optimisticDetach(store.graph.data!.edges[0], true)
    await vi.waitFor(() => expect(detachEdge).toHaveBeenCalledTimes(1))
    store.graph.beginSwitch('other', 0)
    store.graph.receiveSwitch(false)
    expect(store.graph.data?.edges[0].detached).toBe(false)
    expect(store.graph.canMutate).toBe(false)
    fetchGraph.mockResolvedValue(graph(false))
    ack.resolve({ ok: false, data: { error: 'failed original mutation' } })
    expect(await operation).toBe(false)
    await vi.waitFor(() => expect(store.graph.canMutate).toBe(true))
    expect(store.graph.data?.edges[0].detached).toBe(false)
  })

})
