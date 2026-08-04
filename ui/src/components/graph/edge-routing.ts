import type { GraphEdge } from '../../lib/types.gen'
import type { PositionedNode } from './layout'
import { filterVisibleEdges } from './edge-filter'

// Used by GraphView and DependencyEdge to carry canvas-only attachment ports.
export type RoutedGraphEdge = GraphEdge & {
  sourceOffset: number
  targetOffset: number
}

type NodePosition = { x: number; y: number }

const MAX_PORT_SPAN = 112
const MAX_PORT_GAP = 48

export function dependencyEdgeID(edge: GraphEdge): string {
  return `${edge.from}->${edge.to}:${edge.kind === 'async' ? edge.topic : 'sync'}`
}

export function routeDependencyEdges(
  edges: GraphEdge[],
  positions: ReadonlyMap<string, NodePosition>,
): RoutedGraphEdge[] {
  const sourceOffsets = endpointOffsets(edges, edge => edge.from, edge => edge.to, positions)
  const targetOffsets = endpointOffsets(edges, edge => edge.to, edge => edge.from, positions)

  return edges.map(edge => ({
    ...edge,
    sourceOffset: sourceOffsets.get(dependencyEdgeID(edge)) ?? 0,
    targetOffset: targetOffsets.get(dependencyEdgeID(edge)) ?? 0,
  }))
}

export function routeVisibleDependencyEdges(
  edges: GraphEdge[],
  selectedNode: string | null,
  nodes: PositionedNode[],
): RoutedGraphEdge[] {
  const visibleEdges = filterVisibleEdges(edges, selectedNode)
  const parents = new Map(nodes.filter(node => !node.parentId).map(node => [node.id, node.position]))
  const positions = new Map(nodes.map(node => {
    const parent = node.parentId ? parents.get(node.parentId) : undefined
    return [node.id, {
      x: node.position.x + (parent?.x ?? 0),
      y: node.position.y + (parent?.y ?? 0),
    }]
  }))
  return routeDependencyEdges(visibleEdges, positions)
}

function endpointOffsets(
  edges: GraphEdge[],
  endpointKey: (edge: GraphEdge) => string,
  oppositeNode: (edge: GraphEdge) => string,
  positions: ReadonlyMap<string, NodePosition>,
): Map<string, number> {
  const groups = new Map<string, GraphEdge[]>()
  for (const edge of edges) {
    const key = endpointKey(edge)
    const group = groups.get(key) ?? []
    group.push(edge)
    groups.set(key, group)
  }

  const offsets = new Map<string, number>()
  for (const group of groups.values()) {
    group.sort((left, right) => compareOppositeNodes(left, right, oppositeNode, positions))
    const gap = group.length > 1 ? Math.min(MAX_PORT_GAP, MAX_PORT_SPAN / (group.length - 1)) : 0
    const first = -gap * (group.length - 1) / 2
    group.forEach((edge, index) => offsets.set(dependencyEdgeID(edge), first + gap * index))
  }
  return offsets
}

function compareOppositeNodes(
  left: GraphEdge,
  right: GraphEdge,
  oppositeNode: (edge: GraphEdge) => string,
  positions: ReadonlyMap<string, NodePosition>,
): number {
  const leftPosition = positions.get(oppositeNode(left))
  const rightPosition = positions.get(oppositeNode(right))
  if (!leftPosition && !rightPosition) return dependencyEdgeID(left).localeCompare(dependencyEdgeID(right))
  if (!leftPosition) return 1
  if (!rightPosition) return -1
  return leftPosition.x - rightPosition.x
    || leftPosition.y - rightPosition.y
    || dependencyEdgeID(left).localeCompare(dependencyEdgeID(right))
}
