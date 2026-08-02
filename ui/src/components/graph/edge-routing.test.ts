import { describe, expect, it } from 'vitest'
import type { GraphEdge } from '../../lib/types.gen'
import { dependencyEdgeID, routeDependencyEdges } from './edge-routing'

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

describe('routeDependencyEdges', () => {
  const positions = new Map([
    ['demo-shop', { x: 0, y: 0 }],
    ['shop-catalog-api', { x: 0, y: 124 }],
    ['shop-inventory-api', { x: 0, y: 248 }],
    ['shop-order-api', { x: 0, y: 372 }],
    ['redis', { x: 0, y: 620 }],
  ])

  it('keeps the shortest shared-source edge straight and fans longer edges', () => {
    const routed = routeDependencyEdges(quickstartEdges, positions)
    const fromDemoShop = routed.filter(edge => edge.from === 'demo-shop')
    const fromOrder = routed.filter(edge => edge.from === 'shop-order-api')

    expect(new Set(fromDemoShop.map(edge => edge.routeLane)).size).toBe(3)
    expect(fromDemoShop.find(edge => edge.to === 'shop-catalog-api')?.routeLane).toBe(0)
    expect(new Set(fromOrder.map(edge => edge.routeLane)).size).toBe(2)
    expect(fromOrder.every(edge => edge.routeLane !== 0)).toBe(true)
  })

  it('keeps lanes stable when API edge order changes', () => {
    const lanes = (edges: GraphEdge[]) => Object.fromEntries(
      routeDependencyEdges(edges, positions).map(edge => [dependencyEdgeID(edge), edge.routeLane]),
    )

    expect(lanes([...quickstartEdges].reverse())).toEqual(lanes(quickstartEdges))
  })
})
