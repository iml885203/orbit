import { describe, expect, it } from 'vitest'
import { replaceGraphData, store } from './stores.svelte'
import type { GraphResponse } from './types.gen'

const graph = (state = 'healthy'): GraphResponse => ({
  env: 'preview',
  previewOnly: false,
  nodes: [
    { name: 'api', kind: 'backend', state },
    { name: 'web', kind: 'frontend', state },
  ],
  edges: [
    { from: 'web', to: 'api', kind: 'sync', detached: false, detachable: true, env_vars: [] },
  ],
})

describe('replaceGraphData', () => {
  it('keeps the existing graph reference when a status tick returns identical graph data', () => {
    const first = graph()
    store.graph.data = first
    const existing = store.graph.data

    replaceGraphData(graph())

    expect(store.graph.data).toBe(existing)
  })

  it('replaces the graph reference when node state changes', () => {
    const first = graph('starting')
    store.graph.data = first

    replaceGraphData(graph('healthy'))

    expect(store.graph.data).not.toBe(first)
    expect(store.graph.data?.nodes[0].state).toBe('healthy')
  })
})
