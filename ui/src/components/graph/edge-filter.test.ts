import { describe, it, expect } from 'vitest'
import { filterVisibleEdges } from './edge-filter'
import type { GraphEdge } from '../../lib/types.gen'

const sync = (from: string, to: string): GraphEdge => ({
  from, to, kind: 'sync', detached: false, detachable: false, env_vars: [],
})
const async_ = (from: string, to: string, topic: string): GraphEdge => ({
  from, to, kind: 'async', topic, detached: false, detachable: false, env_vars: [],
})

describe('filterVisibleEdges', () => {
  it('keeps all sync edges regardless of selection', () => {
    const edges = [sync('a', 'b'), sync('b', 'c')]
    expect(filterVisibleEdges(edges, null)).toEqual(edges)
    expect(filterVisibleEdges(edges, 'a')).toEqual(edges)
  })

  it('hides async edges when no node is selected', () => {
    const edges = [sync('a', 'b'), async_('a', 'c', 't')]
    const out = filterVisibleEdges(edges, null)
    expect(out).toHaveLength(1)
    expect(out[0].kind).toBe('sync')
  })

  it('reveals async edges touching the selected node (incoming)', () => {
    const edges = [async_('a', 'b', 't1'), async_('a', 'c', 't2')]
    const out = filterVisibleEdges(edges, 'b')
    expect(out).toHaveLength(1)
    expect(out[0].to).toBe('b')
  })

  it('reveals async edges touching the selected node (outgoing)', () => {
    const edges = [async_('a', 'b', 't1'), async_('a', 'c', 't2')]
    const out = filterVisibleEdges(edges, 'a')
    expect(out).toHaveLength(2)
  })

  it('does not include async edges that do not touch the selection', () => {
    const edges = [async_('a', 'b', 't')]
    const out = filterVisibleEdges(edges, 'c')
    expect(out).toHaveLength(0)
  })
})
