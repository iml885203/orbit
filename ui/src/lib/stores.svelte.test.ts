import { describe, expect, it } from 'vitest'
import { hydrateLogs, replaceGraphData, store } from './stores.svelte'
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

  it('replaces the graph when buffered logs become available', () => {
    const first = graph()
    store.graph.data = first
    const next = graph()
    next.nodes[0].logsAvailable = true

    replaceGraphData(next)

    expect(store.graph.data).not.toBe(first)
    expect(store.graph.data?.nodes[0].logsAvailable).toBe(true)
  })
})

describe('hydrateLogs', () => {
  it('merges a buffered snapshot with newer streamed lines without duplicates', () => {
    store.daemon.logBuffers.api = ['second', 'third']

    hydrateLogs('api', ['first', 'second'])

    expect(store.daemon.logBuffers.api).toEqual(['first', 'second', 'third'])
  })

  it('keeps a complete buffered snapshot when live lines are already its tail', () => {
    store.daemon.logBuffers.api = ['second', 'third']

    hydrateLogs('api', ['first', 'second', 'third'])

    expect(store.daemon.logBuffers.api).toEqual(['first', 'second', 'third'])
  })
})

describe('service view preference', () => {
  it('persists graph and table selections', () => {
    store.ui.setServiceView('table')
    expect(store.ui.serviceView).toBe('table')
    expect(localStorage.getItem('orbit.serviceView')).toBe('table')

    store.ui.setServiceView('graph')
    expect(store.ui.serviceView).toBe('graph')
    expect(localStorage.getItem('orbit.serviceView')).toBe('graph')
  })
})
