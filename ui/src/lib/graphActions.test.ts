import { beforeEach, describe, expect, it, vi } from 'vitest'
import { store } from './stores.svelte'

const { detachEdge, fetchGraph } = vi.hoisted(() => ({
  detachEdge: vi.fn(), fetchGraph: vi.fn(),
}))

vi.mock('./api', () => ({ detachEdge, fetchGraph }))

import { optimisticDetach } from './graphActions'

describe('optimisticDetach', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    store.graph.dispose()
    fetchGraph.mockResolvedValue(null)
    store.daemon.connected = true
    store.daemon.runtimeContext = { epoch: 101, generation: 1, config_path: '/workspace/local/orbit.yaml' }
    store.graph.data = {
      env: 'local', runtime: { epoch: 101, generation: 1, config_path: '/workspace/local/orbit.yaml' },
      nodes: [
        { name: 'shop', kind: 'frontend', state: 'degraded', blockedBy: 'api' },
        { name: 'api', kind: 'backend', state: 'stopped' },
      ],
      edges: [
        { from: 'shop', to: 'api', kind: 'dependency', detachable: true, detached: false },
      ],
    }
    store.graph.publish(store.graph.data)
  })

  it('ignores an older response when detach and reattach overlap', async () => {
    let finishDetach!: (value: unknown) => void
    detachEdge
      .mockReturnValueOnce(new Promise(resolve => { finishDetach = resolve }))
      .mockResolvedValueOnce({ ok: true, data: { message: 'reattached', graph: {
        env: 'local', runtime: { epoch: 101, generation: 1, config_path: '/workspace/local/orbit.yaml' },
        nodes: [
          { name: 'shop', kind: 'frontend', state: 'degraded', blockedBy: 'api' },
          { name: 'api', kind: 'backend', state: 'stopped' },
        ],
        edges: [
          { from: 'shop', to: 'api', kind: 'dependency', detachable: true, detached: false },
        ],
      } } })

    expect(store.graph.canMutate).toBe(true)
    const detach = optimisticDetach(store.graph.data!.edges[0], true)
    const reattach = optimisticDetach(store.graph.data!.edges[0], false)
    await Promise.resolve()
    await vi.waitFor(() => expect(detachEdge).toHaveBeenCalledTimes(1))
    finishDetach({ ok: true, data: { message: 'detached', graph: {
      env: 'local', runtime: { epoch: 101, generation: 1, config_path: '/workspace/local/orbit.yaml' },
      nodes: [
        { name: 'shop', kind: 'frontend', state: 'stopped' },
        { name: 'api', kind: 'backend', state: 'stopped' },
      ],
      edges: [
        { from: 'shop', to: 'api', kind: 'dependency', detachable: true, detached: true },
      ],
    } } })
    await detach
    await reattach

    await vi.waitFor(() => expect(detachEdge).toHaveBeenCalledTimes(2))
    expect(store.graph.data?.edges[0].detached).toBe(false)
    expect(store.graph.data?.nodes[0].blockedBy).toBe('api')
  })

  it('does not apply a completed mutation to a newly selected environment', async () => {
    let finishDetach!: (value: unknown) => void
    detachEdge.mockReturnValueOnce(new Promise(resolve => { finishDetach = resolve }))

    expect(store.graph.canMutate).toBe(true)
    const detach = optimisticDetach(store.graph.data!.edges[0], true)
    store.daemon.runtimeContext = { epoch: 101, generation: 2, config_path: '/workspace/other/orbit.yaml' }
    store.graph.data = {
      env: 'other',
      nodes: [{ name: 'other-shop', kind: 'frontend', state: 'stopped' }],
      edges: [],
    }
    finishDetach({ ok: true, data: { message: 'detached', graph: {
      env: 'local', runtime: { epoch: 101, generation: 1, config_path: '/workspace/local/orbit.yaml' },
      nodes: [{ name: 'shop', kind: 'frontend', state: 'stopped' }],
      edges: [{ from: 'shop', to: 'api', kind: 'dependency', detachable: true, detached: true }],
    } } })
    await detach

    expect(store.graph.data.env).toBe('other')
    expect(store.graph.data.nodes[0].name).toBe('other-shop')
  })

  it('does not send a queued mutation after the environment changes', async () => {
    let finishDetach!: (value: unknown) => void
    detachEdge.mockReturnValueOnce(new Promise(resolve => { finishDetach = resolve }))

    expect(store.graph.canMutate).toBe(true)
    const detach = optimisticDetach(store.graph.data!.edges[0], true)
    const reattach = optimisticDetach(store.graph.data!.edges[0], false)
    await vi.waitFor(() => expect(detachEdge).toHaveBeenCalledTimes(1))
    store.daemon.runtimeContext = { epoch: 101, generation: 2, config_path: '/workspace/other/orbit.yaml' }
    store.graph.data = {
      env: 'other',
      nodes: [{ name: 'other-shop', kind: 'frontend', state: 'stopped' }],
      edges: [],
    }
    finishDetach({ ok: true, data: { message: 'detached', graph: {
      env: 'local', runtime: { epoch: 101, generation: 1, config_path: '/workspace/local/orbit.yaml' },
      nodes: [{ name: 'shop', kind: 'frontend', state: 'stopped' }],
      edges: [{ from: 'shop', to: 'api', kind: 'dependency', detachable: true, detached: true }],
    } } })
    await detach
    await reattach

    await vi.waitFor(() => expect(detachEdge).toHaveBeenCalledTimes(1))
    expect(store.graph.data.env).toBe('other')
  })
})
