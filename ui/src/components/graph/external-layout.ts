import type { GraphNode } from '../../lib/types.gen'
import type { Node } from '@xyflow/svelte'
import type { GraphNodeData, PositionedNode } from './layout'

// Layout knobs for "external" nodes (kind: external) — third-party
// dependencies declared in yaml. Externals don't participate in dagre;
// they're attached above the service they share the most async edges with,
// or fall through to the tail row when there's no clear anchor.
//
// EXTERNAL_GAP_Y = distance above the anchor service.
// EXTERNAL_STACK_GAP = extra vertical room between stacked externals
// that share the same anchor (one anchor can host multiple externals).
export const EXTERNAL_WIDTH = 160
export const EXTERNAL_HEIGHT = 56
const EXTERNAL_GAP_Y = 36
const EXTERNAL_STACK_GAP = 28

type ServicePositions = Map<string, { x: number; y: number }>
type EdgeRef = { from: string; to: string; kind: string }

/**
 * emitAttachedExternalNodes places external nodes above the service they
 * share the most async edges with. Multiple externals attached to the
 * same service stack vertically. Returns the set of placed names so the
 * caller can route unattached ones to the tail row.
 *
 * `nodeWidth` is the rendered width of a service node (passed in so
 * layout.ts owns the dimension constant).
 */
export function emitAttachedExternalNodes(
    out: PositionedNode[],
    externalNodes: GraphNode[],
    edges: EdgeRef[],
    servicePositions: ServicePositions,
    nodeWidth: number,
): Set<string> {
    const placed = new Set<string>()
    const occupied = new Map<string, number>()
    for (const ext of externalNodes) {
        const serviceName = relatedServiceForExternal(ext.name, edges, servicePositions)
        if (!serviceName) continue
        const servicePos = servicePositions.get(serviceName)!
        const slot = occupied.get(serviceName) ?? 0
        occupied.set(serviceName, slot + 1)
        out.push({
            id: ext.name,
            data: ext as GraphNodeData,
            type: 'external' as const,
            position: {
                x: servicePos.x + (nodeWidth - EXTERNAL_WIDTH) / 2,
                y: servicePos.y - EXTERNAL_GAP_Y - EXTERNAL_HEIGHT - slot * (EXTERNAL_HEIGHT + EXTERNAL_STACK_GAP),
            },
        } satisfies Node<GraphNodeData, 'external'>)
        placed.add(ext.name)
    }
    return placed
}

// Pick the service that shares the most async edges with this external;
// ties broken alphabetically for determinism. Returns null when there are
// no async edges touching this external — caller falls back to tail row.
function relatedServiceForExternal(
    externalName: string,
    edges: EdgeRef[],
    servicePositions: ServicePositions,
): string | null {
    const counts = new Map<string, number>()
    for (const e of edges) {
        if (e.kind !== 'async') continue
        const other = e.from === externalName ? e.to : e.to === externalName ? e.from : null
        if (!other) continue
        if (!servicePositions.has(other)) continue
        counts.set(other, (counts.get(other) ?? 0) + 1)
    }
    return [...counts.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))[0]?.[0] ?? null
}
