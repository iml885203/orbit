import type { GraphEdge } from '../../lib/types.gen'

/**
 * filterVisibleEdges decides which edges the canvas should render.
 * Sync edges are always shown. Async edges are hidden by default and
 * surface only when the user has selected a node that one of their
 * endpoints touches — that selection is what NodeDrawer is already
 * keyed on, so closing the drawer (clearing selectedNode) hides them.
 */
export function filterVisibleEdges(
  edges: GraphEdge[],
  selectedNode: string | null,
): GraphEdge[] {
  return edges.filter(e => {
    if (e.kind !== 'async') return true
    if (!selectedNode) return false
    return e.from === selectedNode || e.to === selectedNode
  })
}
