import dagre from '@dagrejs/dagre'
import type { GraphResponse, GraphNode } from '../../lib/types.gen'
import type { Node } from '@xyflow/svelte'
import type { LayoutMode } from '../../lib/stores.svelte'
import { emitAttachedExternalNodes } from './external-layout'

// Match ServiceNode.svelte's rendered size (width: 240, min-height: 92).
// dagre uses these to compute spacing; if they understate the real size, nodes overlap.
const NODE_WIDTH = 240
const NODE_HEIGHT = 92

// Padding inside a group box (between the border and inner nodes).
// Top padding leaves room for the group header (label + start/stop buttons);
// it must clear the header band or the first service row covers it, since
// service nodes render on top of the group box.
const GROUP_PAD_X = 24
const GROUP_PAD_TOP = 56
const GROUP_PAD_BOTTOM = 24

// Gaps between nodes when a group's interior is laid out as a grid.
const GRID_GAP_X = 40
const GRID_GAP_Y = 32

// Layout passes ignore async edges so their presence doesn't shift node
// positions — async edges render on top, layout is driven entirely by
// synchronous startup deps.
function isSyncEdge(e: { kind: string }): boolean {
  return e.kind === 'sync'
}

export type GraphNodeData = GraphNode & Record<string, unknown>
export type GroupNodeData = { name: string; color?: string; serviceCount: number }
export type PositionedNode =
  | Node<GraphNodeData, 'service'>
  | Node<GroupNodeData, 'group'>
  | Node<GraphNodeData, 'external'>

/**
 * layout returns positioned nodes for SvelteFlow rendering.
 *
 * Clustering kicks in whenever the yaml declares `groups:` — the author
 * opted into visual grouping. Envs without groups fall through to flat
 * dagre (the original behaviour).
 */
export function layout(graph: GraphResponse, mode: LayoutMode = 'rectangle'): PositionedNode[] {
  if (graph.nodes.length === 0) return []
  if (graph.groups && graph.groups.length > 0) {
    return clusteredLayout(graph, mode)
  }
  return flatLayout(graph)
}

// Shared dagre options. nodesep/ranksep are passed in by callers because
// the flat layout wants more breathing room than per-cluster passes do.
type DagreOpts = { nodesep: number; ranksep: number }

// Meta-graph node label after dagre.layout: dagre populates x/y in place
// on each node value. The package's generic typing falls through to
// `unknown`, so we re-state the shape we rely on here.
type MetaNode = { x: number; y: number }
function metaNodeAt(g: InstanceType<typeof dagre.graphlib.Graph>, name: string): MetaNode {
  return g.node(name) as MetaNode
}

/**
 * runDagre positions nodes with dagre and returns their {x, y} centres.
 * Only edges where both endpoints are in `nodeNames` are considered;
 * callers pass an already-filtered visible set so dagre doesn't try to
 * lay out absent nodes.
 */
function runDagre(
  nodeNames: string[],
  edges: { from: string; to: string; kind: string }[],
  opts: DagreOpts,
): Map<string, { x: number; y: number }> {
  const g = new dagre.graphlib.Graph()
  g.setGraph({ rankdir: 'TB', nodesep: opts.nodesep, ranksep: opts.ranksep })
  g.setDefaultEdgeLabel(() => ({}))

  const visible = new Set(nodeNames)
  // Async edges are hidden by default and revealed on selection; excluding
  // them from dagre keeps node positions stable when they light up.
  const syncEdges = edges.filter(isSyncEdge)
  for (const name of nodeNames) {
    g.setNode(name, { width: NODE_WIDTH, height: NODE_HEIGHT })
  }
  for (const e of syncEdges) {
    if (!visible.has(e.from) || !visible.has(e.to)) continue
    g.setEdge(e.from, e.to)
  }
  dagre.layout(g)

  const out = new Map<string, { x: number; y: number }>()
  for (const name of nodeNames) {
    const p = g.node(name)
    out.set(name, { x: p.x, y: p.y })
  }
  return out
}

function flatLayout(graph: GraphResponse): PositionedNode[] {
  const renderNodes = graph.nodes
  const positions = runDagre(
    renderNodes.map(n => n.name),
    graph.edges,
    { nodesep: 80, ranksep: 100 },
  )

  return renderNodes.map(n => {
    const pos = positions.get(n.name)!
    const position = { x: pos.x - NODE_WIDTH / 2, y: pos.y - NODE_HEIGHT / 2 }
    if (n.kind === 'external') {
      return {
        id: n.name,
        data: n as GraphNodeData,
        type: 'external' as const,
        position,
      } satisfies Node<GraphNodeData, 'external'>
    }
    return {
      id: n.name,
      data: n as GraphNodeData,
      type: 'service' as const,
      position,
    } satisfies Node<GraphNodeData, 'service'>
  })
}

// One cluster's laid-out interior. Positions are top-left of each child
// node, relative to (0,0) inside the cluster — the parent's screen-space
// origin is added in emitClusterChildren.
type ClusterLayout = {
  name: string
  width: number
  height: number
  positions: Map<string, { x: number; y: number }>
}

function clusteredLayout(graph: GraphResponse, mode: LayoutMode): PositionedNode[] {
  const groupOf = new Map<string, string>()    // service name → group name
  const colorOf = new Map<string, string>()    // group name → yaml color (if any)
  for (const g of graph.groups!) {
    if (g.color) colorOf.set(g.name, g.color)
    for (const s of g.services) groupOf.set(s, g.name)
  }

  const infraNodes = graph.nodes.filter(n => n.kind === 'infra')
  const externalNodes = graph.nodes.filter(n => n.kind === 'external')
  // Services assigned to a group are clustered; the rest fall back to a
  // tail row alongside any visible infra and external nodes.
  const groupedSvcs = new Map<string, GraphNode[]>()
  const orphanSvcs: GraphNode[] = []
  for (const n of graph.nodes) {
    if (n.kind === 'infra' || n.kind === 'external') continue
    const grp = groupOf.get(n.name)
    if (!grp) {
      orphanSvcs.push(n)
      continue
    }
    if (!groupedSvcs.has(grp)) groupedSvcs.set(grp, [])
    groupedSvcs.get(grp)!.push(n)
  }

  const clusters = layoutClusters(graph, groupedSvcs, mode)
  const meta = layoutMetaGraph(clusters, graph.edges, groupOf)
  const groupedServicePositions = absoluteGroupedServicePositions(clusters, groupedSvcs, meta)

  const out: PositionedNode[] = []
  for (const c of clusters) {
    emitClusterParent(out, c, meta, colorOf)
    emitClusterChildren(out, c, groupedSvcs.get(c.name)!)
  }
  const placedExternals = emitAttachedExternalNodes(out, externalNodes, graph.edges, groupedServicePositions, NODE_WIDTH)
  const unattachedExternals = externalNodes.filter(n => !placedExternals.has(n.name))
  emitTailRow(
    out,
    [...infraNodes, ...orphanSvcs, ...unattachedExternals],
    clusters,
    meta,
    graph.edges,
    groupedServicePositions,
  )
  return out
}

/**
 * gridColumnsFor picks the column count whose resulting block is closest to
 * square. Nodes are wide (240×92), so a plain sqrt(n) grid still renders far
 * wider than tall; we instead try every column count and keep the one that
 * minimises |blockWidth − blockHeight|. This stops large groups (e.g. the 18
 * game providers, which share no intra-group rank) from spreading into one
 * very wide row.
 */
function gridColumnsFor(n: number): number {
  let best = 1
  let bestDiff = Infinity
  for (let cols = 1; cols <= n; cols++) {
    const rows = Math.ceil(n / cols)
    const w = cols * NODE_WIDTH + (cols - 1) * GRID_GAP_X
    const h = rows * NODE_HEIGHT + (rows - 1) * GRID_GAP_Y
    const diff = Math.abs(w - h)
    if (diff < bestDiff) {
      bestDiff = diff
      best = cols
    }
  }
  return best
}

// Phase 1: per-group interior layout. 'rectangle' packs edgeless siblings
// into a near-square grid, but keeps dependency-connected groups in dagre
// ranks so distinct edges do not collapse onto a single vertical spine.
// 'extend' uses dagre for every group.
function layoutClusters(
  graph: GraphResponse,
  groupedSvcs: Map<string, GraphNode[]>,
  mode: LayoutMode,
): ClusterLayout[] {
  const clusters: ClusterLayout[] = []
  for (const grp of graph.groups!) {
    const svcs = groupedSvcs.get(grp.name)
    if (!svcs || svcs.length === 0) continue
    const positions = mode === 'extend' || hasIntraGroupDependencies(graph, svcs)
      ? dagreInteriorPositions(graph, svcs)
      : gridInteriorPositions(svcs)
    let maxX = 0
    let maxY = 0
    for (const p of positions.values()) {
      maxX = Math.max(maxX, p.x + NODE_WIDTH)
      maxY = Math.max(maxY, p.y + NODE_HEIGHT)
    }
    clusters.push({
      name: grp.name,
      width: maxX + GROUP_PAD_X * 2,
      height: maxY + GROUP_PAD_TOP + GROUP_PAD_BOTTOM,
      positions,
    })
  }
  return clusters
}

function hasIntraGroupDependencies(graph: GraphResponse, svcs: GraphNode[]): boolean {
  const names = new Set(svcs.map(service => service.name))
  return graph.edges.some(edge => isSyncEdge(edge) && names.has(edge.from) && names.has(edge.to))
}

// 'rectangle': row-major near-square grid of top-left positions.
function gridInteriorPositions(svcs: GraphNode[]): Map<string, { x: number; y: number }> {
  const cols = gridColumnsFor(svcs.length)
  const positions = new Map<string, { x: number; y: number }>()
  svcs.forEach((s, i) => {
    const col = i % cols
    const row = Math.floor(i / cols)
    positions.set(s.name, {
      x: col * (NODE_WIDTH + GRID_GAP_X),
      y: row * (NODE_HEIGHT + GRID_GAP_Y),
    })
  })
  return positions
}

// 'extend': dependency-ordered dagre layout (top-left positions), the
// original per-group behaviour before grid packing.
function dagreInteriorPositions(
  graph: GraphResponse,
  svcs: GraphNode[],
): Map<string, { x: number; y: number }> {
  const centres = runDagre(
    svcs.map(s => s.name),
    graph.edges,
    { nodesep: 40, ranksep: 60 },
  )
  const positions = new Map<string, { x: number; y: number }>()
  for (const s of svcs) {
    const c = centres.get(s.name)!
    positions.set(s.name, { x: c.x - NODE_WIDTH / 2, y: c.y - NODE_HEIGHT / 2 })
  }
  return positions
}

// Phase 2: dagre between clusters. An edge A → B exists if any service in
// cluster A depends on any service in cluster B. Each cluster is treated
// as a single dagre node sized by its laid-out interior so neighbouring
// clusters don't overlap.
function layoutMetaGraph(
  clusters: ClusterLayout[],
  edges: { from: string; to: string; kind: string }[],
  groupOf: Map<string, string>,
): InstanceType<typeof dagre.graphlib.Graph> {
  const meta = new dagre.graphlib.Graph()
  meta.setGraph({ rankdir: 'TB', nodesep: 60, ranksep: 80 })
  meta.setDefaultEdgeLabel(() => ({}))
  for (const c of clusters) meta.setNode(c.name, { width: c.width, height: c.height })
  const seen = new Set<string>()
  // Same rationale as runDagre: async edges are hidden by default, so
  // excluding them keeps cluster positions stable.
  const syncEdges = edges.filter(isSyncEdge)
  for (const e of syncEdges) {
    const fromG = groupOf.get(e.from)
    const toG = groupOf.get(e.to)
    if (!fromG || !toG || fromG === toG) continue
    const k = `${fromG}->${toG}`
    if (seen.has(k)) continue
    seen.add(k)
    meta.setEdge(fromG, toG)
  }
  dagre.layout(meta)
  return meta
}

function emitClusterParent(
  out: PositionedNode[],
  c: ClusterLayout,
  meta: InstanceType<typeof dagre.graphlib.Graph>,
  colorOf: Map<string, string>,
): void {
  const m = metaNodeAt(meta, c.name)
  out.push({
    id: `group:${c.name}`,
    data: { name: c.name, color: colorOf.get(c.name), serviceCount: c.positions.size },
    type: 'group' as const,
    position: { x: m.x - c.width / 2, y: m.y - c.height / 2 },
    // SvelteFlow needs explicit width/height for non-default node types.
    width: c.width,
    height: c.height,
    // Group should not capture clicks meant for child nodes.
    selectable: false,
    draggable: false,
  } as Node<GroupNodeData, 'group'>)
}

function emitClusterChildren(
  out: PositionedNode[],
  c: ClusterLayout,
  svcs: GraphNode[],
): void {
  const byName = new Map(svcs.map(s => [s.name, s]))
  for (const [svcName, pos] of c.positions) {
    out.push({
      id: svcName,
      data: byName.get(svcName)! as GraphNodeData,
      type: 'service' as const,
      // Position is relative to the parent group when parentId is set.
      position: { x: pos.x + GROUP_PAD_X, y: pos.y + GROUP_PAD_TOP },
      parentId: `group:${c.name}`,
      extent: 'parent',
    } as Node<GraphNodeData, 'service'>)
  }
}

function absoluteGroupedServicePositions(
  clusters: ClusterLayout[],
  groupedSvcs: Map<string, GraphNode[]>,
  meta: InstanceType<typeof dagre.graphlib.Graph>,
): Map<string, { x: number; y: number }> {
  const out = new Map<string, { x: number; y: number }>()
  for (const c of clusters) {
    const m = metaNodeAt(meta, c.name)
    const clusterX = m.x - c.width / 2
    const clusterY = m.y - c.height / 2
    const svcs = groupedSvcs.get(c.name) ?? []
    for (const svc of svcs) {
      const pos = c.positions.get(svc.name)
      if (!pos) continue
      out.set(svc.name, {
        x: clusterX + pos.x + GROUP_PAD_X,
        y: clusterY + pos.y + GROUP_PAD_TOP,
      })
    }
  }
  return out
}

// Phase 3: tail row of leftover nodes (orphan services + non-hidden infra)
// arranged in a centred horizontal strip below the deepest cluster.
function emitTailRow(
  out: PositionedNode[],
  tail: GraphNode[],
  clusters: ClusterLayout[],
  meta: InstanceType<typeof dagre.graphlib.Graph>,
  edges: { from: string; to: string; kind: string }[],
  groupedServicePositions: ReadonlyMap<string, { x: number; y: number }>,
): void {
  if (tail.length === 0) return
  const bottomY = clusters.reduce((acc, c) => {
    const m = metaNodeAt(meta, c.name)
    return Math.max(acc, m.y + c.height / 2)
  }, 0) + 120
  const totalWidth = tail.length * NODE_WIDTH + Math.max(0, tail.length - 1) * 60
  const clusterLeft = clusters.reduce((left, cluster) => {
    const node = metaNodeAt(meta, cluster.name)
    return Math.min(left, node.x - cluster.width / 2)
  }, Infinity)
  const clusterRight = clusters.reduce((right, cluster) => {
    const node = metaNodeAt(meta, cluster.name)
    return Math.max(right, node.x + cluster.width / 2)
  }, -Infinity)
  const clusterCenterX = clusters.length > 0 ? (clusterLeft + clusterRight) / 2 : 0
  const soleInfra = tail.length === 1 && tail[0].kind === 'infra' ? tail[0] : null
  const infraSources = soleInfra
    ? edges.filter(edge => isSyncEdge(edge) && edge.to === soleInfra.name && groupedServicePositions.has(edge.from))
    : []
  const sourcePosition = infraSources.length === 1 ? groupedServicePositions.get(infraSources[0].from) : null
  const tailCenterX = sourcePosition ? sourcePosition.x + NODE_WIDTH / 2 : clusterCenterX
  let startX = tailCenterX - totalWidth / 2
  for (const n of tail) {
    if (n.kind === 'external') {
      out.push({
        id: n.name,
        data: n as GraphNodeData,
        type: 'external' as const,
        position: { x: startX, y: bottomY },
      } satisfies Node<GraphNodeData, 'external'>)
    } else {
      out.push({
        id: n.name,
        data: n as GraphNodeData,
        type: 'service' as const,
        position: { x: startX, y: bottomY },
      } satisfies Node<GraphNodeData, 'service'>)
    }
    startX += NODE_WIDTH + 60
  }
}
