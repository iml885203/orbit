import { describe, expect, it, vi } from 'vitest'
import type { GraphEdge } from '../../lib/types.gen'
import { dependencyEdgeID, routeDependencyEdges, routeVisibleDependencyEdges } from './edge-routing'
import { layout } from './layout'
import { buildDependencyPath } from './edge-path'
import type { GraphResponse } from '../../lib/types.gen'

vi.mock('@xyflow/svelte', () => ({
  Position: { Top: 'top', Bottom: 'bottom' },
  getBezierPath: ({ sourceX, sourceY, targetX, targetY }: {
    sourceX: number; sourceY: number; targetX: number; targetY: number
  }) => [`M${sourceX},${sourceY} L${targetX},${targetY}`, (sourceX + targetX) / 2, (sourceY + targetY) / 2],
}))

const sync = (from: string, to: string): GraphEdge => ({
  from,
  to,
  kind: 'sync',
  detached: false,
  detachable: false,
  env_vars: [],
})

const quickstartEdges = [
  sync('demo-shop', 'shop-catalog-api'),
  sync('demo-shop', 'shop-inventory-api'),
  sync('demo-shop', 'shop-order-api'),
  sync('shop-order-api', 'shop-catalog-api'),
  sync('shop-order-api', 'shop-inventory-api'),
  sync('shop-inventory-api', 'redis'),
]

const positions = new Map([
  ['demo-shop', { x: 280, y: 0 }],
  ['shop-order-api', { x: 0, y: 152 }],
  ['shop-inventory-api', { x: 60, y: 304 }],
  ['shop-catalog-api', { x: 340, y: 304 }],
  ['redis', { x: 60, y: 548 }],
])

describe('routeDependencyEdges', () => {
  it('fans every shared source and target across distinct node ports', () => {
    const routed = routeDependencyEdges(quickstartEdges, positions)
    const fromDemo = routed.filter(edge => edge.from === 'demo-shop')
    const intoInventory = routed.filter(edge => edge.to === 'shop-inventory-api')

    expect(new Set(fromDemo.map(edge => edge.sourceOffset)).size).toBe(3)
    expect(new Set(intoInventory.map(edge => edge.targetOffset)).size).toBe(2)
    expect(routed.find(edge => edge.from === 'shop-inventory-api')?.sourceOffset).toBe(0)
    expect(routed.find(edge => edge.to === 'redis')?.targetOffset).toBe(0)
  })

  it('keeps port assignments stable when API edge order changes', () => {
    const offsets = (edges: GraphEdge[]) => Object.fromEntries(
      routeDependencyEdges(edges, positions).map(edge => [dependencyEdgeID(edge), [edge.sourceOffset, edge.targetOffset]]),
    )

    expect(offsets([...quickstartEdges].reverse())).toEqual(offsets(quickstartEdges))
  })

  it('routes only the async edges visible for the selected node', () => {
    const asyncEdges: GraphEdge[] = ['one', 'two', 'three'].map(to => ({
      from: 'producer', to, kind: 'async', topic: to, detached: false, detachable: false, env_vars: [],
    }))
    const nodes = layout({
      env: 'async',
      nodes: ['producer', 'one', 'two', 'three'].map(name => ({ name, kind: 'backend', state: 'healthy' })),
      edges: asyncEdges,
    })

    const [visible] = routeVisibleDependencyEdges(asyncEdges, 'one', nodes)
    expect(visible.sourceOffset).toBe(0)
    expect(visible.targetOffset).toBe(0)
  })

  it('separates selected async edges from sync edges at the same endpoint', () => {
    const mixedEdges: GraphEdge[] = [
      sync('producer', 'database'),
      { from: 'producer', to: 'consumer', kind: 'async', topic: 'events', detached: false, detachable: false, env_vars: [] },
    ]
    const nodes = layout({
      env: 'mixed',
      nodes: ['producer', 'database', 'consumer'].map(name => ({ name, kind: 'backend', state: 'healthy' })),
      edges: mixedEdges,
    })

    const routed = routeVisibleDependencyEdges(mixedEdges, 'consumer', nodes)
    expect(routed).toHaveLength(2)
    expect(new Set(routed.map(edge => edge.sourceOffset)).size).toBe(2)
  })

  it('composes the orbit-demo layout into six distinct dependency paths', () => {
    const demoGraph: GraphResponse = {
      env: 'orbit-demo',
      groups: [{ name: 'mini-shop', services: ['demo-shop', 'shop-catalog-api', 'shop-inventory-api', 'shop-order-api'] }],
      nodes: [
        { name: 'demo-shop', kind: 'frontend', state: 'healthy' },
        { name: 'shop-catalog-api', kind: 'backend', state: 'healthy' },
        { name: 'shop-inventory-api', kind: 'backend', state: 'healthy' },
        { name: 'shop-order-api', kind: 'backend', state: 'healthy' },
        { name: 'redis', kind: 'infra', state: 'healthy' },
      ],
      edges: quickstartEdges,
    }
    const nodes = layout(demoGraph)
    const routed = routeVisibleDependencyEdges(demoGraph.edges, null, nodes)
    const parents = new Map(nodes.filter(node => !node.parentId).map(node => [node.id, node.position]))
    const absolute = new Map(nodes.map(node => {
      const parent = node.parentId ? parents.get(node.parentId) : undefined
      return [node.id, {
        x: node.position.x + (parent?.x ?? 0),
        y: node.position.y + (parent?.y ?? 0),
      }]
    }))
    const paths = routed.map(edge => {
      const source = absolute.get(edge.from)!
      const target = absolute.get(edge.to)!
      return { edge, path: buildDependencyPath({
        id: dependencyEdgeID(edge), async: false,
        sourceX: source.x + 120, sourceY: source.y + 92,
        targetX: target.x + 120, targetY: target.y,
        sourceOffset: edge.sourceOffset, targetOffset: edge.targetOffset,
      }).path }
    })
    const starts = paths
      .filter(({ edge }) => edge.from === 'demo-shop')
      .map(({ path }) => path.match(/^M([^,]+)/)?.[1])
    const inventoryEnds = paths
      .filter(({ edge }) => edge.to === 'shop-inventory-api')
      .map(({ path }) => path.match(/L([^,]+),[^ ]+$/)?.[1])

    expect(routed).toHaveLength(6)
    expect(new Set(starts).size).toBe(3)
    expect(new Set(inventoryEnds).size).toBe(2)
  })
})
