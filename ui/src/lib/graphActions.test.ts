import { beforeEach, describe, expect, it, vi } from 'vitest'
import { store } from './stores.svelte'

const { detachEdge } = vi.hoisted(() => ({
  detachEdge: vi.fn(),
}))

vi.mock('./api', () => ({ detachEdge }))

import { optimisticDetach } from './graphActions'

describe('optimisticDetach', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    store.graph.data = {
      env: 'local',
      nodes: [
        { name: 'shop', kind: 'frontend', state: 'degraded', blockedBy: 'api' },
        { name: 'api', kind: 'backend', state: 'stopped' },
      ],
      edges: [
        { from: 'shop', to: 'api', kind: 'dependency', detachable: true, detached: false },
      ],
    }
  })

  it('refreshes node lifecycle actions after detaching and reattaching', async () => {
    detachEdge
      .mockResolvedValueOnce({ ok: true, data: { message: 'updated', graph: {
        env: 'local',
        nodes: [
          { name: 'shop', kind: 'frontend', state: 'stopped' },
          { name: 'api', kind: 'backend', state: 'stopped' },
        ],
        edges: [
          { from: 'shop', to: 'api', kind: 'dependency', detachable: true, detached: true },
        ],
      } } })
      .mockResolvedValueOnce({ ok: true, data: { message: 'updated', graph: {
        env: 'local',
        nodes: [
          { name: 'shop', kind: 'frontend', state: 'degraded', blockedBy: 'api' },
          { name: 'api', kind: 'backend', state: 'stopped' },
        ],
        edges: [
          { from: 'shop', to: 'api', kind: 'dependency', detachable: true, detached: false },
        ],
      } } })

    await optimisticDetach(store.graph.data!.edges[0], true)
    expect(store.graph.data?.nodes[0]).toMatchObject({ name: 'shop', state: 'stopped' })
    expect(store.graph.data?.nodes[0].blockedBy).toBeUndefined()

    await optimisticDetach(store.graph.data!.edges[0], false)
    expect(store.graph.data?.nodes[0]).toMatchObject({
      name: 'shop',
      state: 'degraded',
      blockedBy: 'api',
    })
  })

  it('ignores an older response when detach and reattach overlap', async () => {
    let finishDetach!: (value: unknown) => void
    detachEdge
      .mockReturnValueOnce(new Promise(resolve => { finishDetach = resolve }))
      .mockResolvedValueOnce({ ok: true, data: { message: 'reattached', graph: {
        env: 'local',
        nodes: [
          { name: 'shop', kind: 'frontend', state: 'degraded', blockedBy: 'api' },
          { name: 'api', kind: 'backend', state: 'stopped' },
        ],
        edges: [
          { from: 'shop', to: 'api', kind: 'dependency', detachable: true, detached: false },
        ],
      } } })

    const detach = optimisticDetach(store.graph.data!.edges[0], true)
    const reattach = optimisticDetach(store.graph.data!.edges[0], false)
    await Promise.resolve()
    expect(detachEdge).toHaveBeenCalledTimes(1)
    finishDetach({ ok: true, data: { message: 'detached', graph: {
      env: 'local',
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

    expect(detachEdge).toHaveBeenCalledTimes(2)
    expect(store.graph.data?.edges[0].detached).toBe(false)
    expect(store.graph.data?.nodes[0].blockedBy).toBe('api')
  })

  it('does not apply a completed mutation to a newly selected environment', async () => {
    let finishDetach!: (value: unknown) => void
    detachEdge.mockReturnValueOnce(new Promise(resolve => { finishDetach = resolve }))

    const detach = optimisticDetach(store.graph.data!.edges[0], true)
    store.graph.data = {
      env: 'other',
      nodes: [{ name: 'other-shop', kind: 'frontend', state: 'stopped' }],
      edges: [],
    }
    finishDetach({ ok: true, data: { message: 'detached', graph: {
      env: 'local',
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

    const detach = optimisticDetach(store.graph.data!.edges[0], true)
    const reattach = optimisticDetach(store.graph.data!.edges[0], false)
    await Promise.resolve()
    store.graph.data = {
      env: 'other',
      nodes: [{ name: 'other-shop', kind: 'frontend', state: 'stopped' }],
      edges: [],
    }
    finishDetach({ ok: true, data: { message: 'detached', graph: {
      env: 'local',
      nodes: [{ name: 'shop', kind: 'frontend', state: 'stopped' }],
      edges: [{ from: 'shop', to: 'api', kind: 'dependency', detachable: true, detached: true }],
    } } })
    await detach
    await reattach

    expect(detachEdge).toHaveBeenCalledTimes(1)
    expect(store.graph.data.env).toBe('other')
  })
})
