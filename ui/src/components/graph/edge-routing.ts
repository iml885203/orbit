import type { GraphEdge } from '../../lib/types.gen'

// Used by GraphView and DependencyEdge to carry canvas-only routing state.
export type RoutedGraphEdge = GraphEdge & { routeLane: number }

type NodePosition = { x: number; y: number }

export function dependencyEdgeID(edge: GraphEdge): string {
  return `${edge.from}->${edge.to}:${edge.kind === 'async' ? edge.topic : 'sync'}`
}

export function routeDependencyEdges(
  edges: GraphEdge[],
  positions: ReadonlyMap<string, NodePosition>,
): RoutedGraphEdge[] {
  const sourceLanes = lanesByEndpoint(edges, edge => `${edge.kind}:${edge.from}`, positions)
  const targetLanes = lanesByEndpoint(edges, edge => `${edge.kind}:${edge.to}`, positions)

  return edges.map(edge => ({
    ...edge,
    routeLane: sourceLanes.get(dependencyEdgeID(edge))
      ?? targetLanes.get(dependencyEdgeID(edge))
      ?? 0,
  }))
}

function lanesByEndpoint(
  edges: GraphEdge[],
  endpointKey: (edge: GraphEdge) => string,
  positions: ReadonlyMap<string, NodePosition>,
): Map<string, number> {
  const groups = new Map<string, GraphEdge[]>()
  for (const edge of edges) {
    const key = endpointKey(edge)
    const group = groups.get(key) ?? []
    group.push(edge)
    groups.set(key, group)
  }

  const lanes = new Map<string, number>()
  for (const group of groups.values()) {
    if (group.length < 2) continue
    group.sort((a, b) => Number(!flowsDown(a, positions)) - Number(!flowsDown(b, positions))
      || edgeLength(a, positions) - edgeLength(b, positions)
      || dependencyEdgeID(a).localeCompare(dependencyEdgeID(b)))
    let routedIndex = 0
    group.forEach((edge, index) => {
      const lane = index === 0 && flowsDown(edge, positions) ? 0 : fanLane(routedIndex++)
      lanes.set(dependencyEdgeID(edge), lane)
    })
  }
  return lanes
}

function fanLane(index: number): number {
  const distance = Math.floor(index / 2) + 1
  return index % 2 === 0 ? -distance : distance
}

function edgeLength(edge: GraphEdge, positions: ReadonlyMap<string, NodePosition>): number {
  const source = positions.get(edge.from)
  const target = positions.get(edge.to)
  if (!source || !target) return 0
  return Math.hypot(target.x - source.x, target.y - source.y)
}

function flowsDown(edge: GraphEdge, positions: ReadonlyMap<string, NodePosition>): boolean {
  const source = positions.get(edge.from)
  const target = positions.get(edge.to)
  return !!source && !!target && target.y > source.y
}
