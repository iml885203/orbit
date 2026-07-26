import { store, toast } from './stores.svelte'
import { detachEdge } from './api'
import type { GraphEdge } from './types.gen'

/**
 * Flip an edge's detached state with optimistic UI: mutate store.graph.data
 * immediately, then call the API; revert on failure. Returns true on
 * success, false on failure (toast already shown).
 */
export async function optimisticDetach(edge: GraphEdge, detached: boolean): Promise<boolean> {
  const env = store.graph.data?.env ?? ''
  const apply = (target: boolean) => {
    if (!store.graph.data) return
    const idx = store.graph.data.edges.findIndex(e => e.from === edge.from && e.to === edge.to)
    if (idx >= 0) store.graph.data.edges[idx] = { ...edge, detached: target }
  }
  apply(detached)
  const { ok, data } = await detachEdge(env, edge.from, edge.to, detached)
  if (!ok) {
    apply(!detached)
    toast(data?.error || 'Failed to update edge')
    return false
  }
  toast(data?.message || (detached ? 'Detached' : 'Reattached'))
  return true
}
