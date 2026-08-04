import { describe, it, expect } from 'vitest'
import { layout } from './layout'
import type { GraphResponse } from '../../lib/types.gen'

const graph: GraphResponse = {
  env: 'development',
  nodes: [
    { name: 'redis', kind: 'infra', state: 'healthy' },
    { name: 'api',  kind: 'backend', state: 'healthy' },
    { name: 'frontend', kind: 'frontend', state: 'healthy' },
  ],
  edges: [
    { from: 'api',  to: 'redis', kind: 'sync', detached: false, detachable: false, env_vars: [] },
    { from: 'frontend', to: 'api',  kind: 'sync', detached: false, detachable: true,  env_vars: [] },
  ],
}

describe('layout', () => {
  it('assigns x/y to every node', () => {
    const positioned = layout(graph)
    expect(positioned).toHaveLength(3)
    positioned.forEach(n => {
      expect(typeof n.position.x).toBe('number')
      expect(typeof n.position.y).toBe('number')
    })
  })

  it('places dependents above their dependencies (frontend top, infra bottom)', () => {
    const pos = Object.fromEntries(layout(graph).map(n => [n.id, n.position.y]))
    // TB rankdir: deps land at higher y; frontend (frontend, dependent) → smallest y
    expect(pos['frontend']).toBeLessThan(pos['api'])
    expect(pos['api']).toBeLessThan(pos['redis'])
  })

  it('handles empty graph', () => {
    const empty: GraphResponse = { env: 'x', nodes: [], edges: [] }
    expect(layout(empty)).toEqual([])
  })

  it('leaves flat graph alignment under dagre control', () => {
    const flat: GraphResponse = {
      env: 'flat',
      nodes: [
        { name: 'root', kind: 'frontend', state: 'healthy' },
        { name: 'middle', kind: 'backend', state: 'healthy' },
        { name: 'left', kind: 'backend', state: 'healthy' },
        { name: 'right', kind: 'backend', state: 'healthy' },
      ],
      edges: [
        { from: 'root', to: 'middle', kind: 'sync', detached: false, detachable: false, env_vars: [] },
        { from: 'root', to: 'left', kind: 'sync', detached: false, detachable: false, env_vars: [] },
        { from: 'root', to: 'right', kind: 'sync', detached: false, detachable: false, env_vars: [] },
        { from: 'middle', to: 'left', kind: 'sync', detached: false, detachable: false, env_vars: [] },
        { from: 'middle', to: 'right', kind: 'sync', detached: false, detachable: false, env_vars: [] },
      ],
    }
    const positions = Object.fromEntries(layout(flat).map(node => [node.id, node.position]))

    expect(positions['root'].x).not.toBe(positions['middle'].x)
  })

  it('uses dependency ranks instead of a single-column grid inside connected groups', () => {
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
      edges: [
        { from: 'demo-shop', to: 'shop-catalog-api', kind: 'sync', detached: false, detachable: false, env_vars: [] },
        { from: 'demo-shop', to: 'shop-inventory-api', kind: 'sync', detached: false, detachable: false, env_vars: [] },
        { from: 'demo-shop', to: 'shop-order-api', kind: 'sync', detached: false, detachable: false, env_vars: [] },
        { from: 'shop-order-api', to: 'shop-catalog-api', kind: 'sync', detached: false, detachable: false, env_vars: [] },
        { from: 'shop-order-api', to: 'shop-inventory-api', kind: 'sync', detached: false, detachable: false, env_vars: [] },
        { from: 'shop-inventory-api', to: 'redis', kind: 'sync', detached: false, detachable: false, env_vars: [] },
      ],
    }

    const positioned = layout(demoGraph)
    const positions = Object.fromEntries(positioned.map(node => [node.id, node.position]))
    const group = positioned.find(node => node.id === 'group:mini-shop')!

    expect(positions['demo-shop'].y).toBeLessThan(positions['shop-order-api'].y)
    expect(positions['shop-order-api'].y).toBeLessThan(positions['shop-catalog-api'].y)
    expect(positions['shop-order-api'].y).toBeLessThan(positions['shop-inventory-api'].y)
    expect(positions['demo-shop'].x).toBe(positions['shop-order-api'].x)
    expect(positions['shop-catalog-api'].x).not.toBe(positions['shop-inventory-api'].x)
    expect(positions['redis'].x + 120).toBe(group.position.x + positions['shop-inventory-api'].x + 120)
  })

  it('places async-connected external nodes above their related consumer service', () => {
    const previewGraph: GraphResponse = {
      env: 'preview',
      groups: [
        { name: 'sports', services: ['odds-api', 'event-consumer'] },
        { name: 'platform', services: ['profile-api'] },
      ],
      nodes: [
        { name: 'odds-api', kind: 'backend', state: 'pending' },
        { name: 'event-consumer', kind: 'backend', state: 'pending' },
        { name: 'profile-api', kind: 'backend', state: 'pending' },
        { name: 'upstream', kind: 'external', state: 'pending', label: 'Upstream' },
      ],
      edges: [
        { from: 'upstream', to: 'event-consumer', kind: 'async', topic: 'upstream.odds', detached: false, detachable: false, env_vars: [] },
      ],
    }

    const positioned = layout(previewGraph)
    const sports = positioned.find(n => n.id === 'group:sports')!
    const consumer = positioned.find(n => n.id === 'event-consumer')!
    const upstream = positioned.find(n => n.id === 'upstream')!
    const consumerCenterX = sports.position.x + consumer.position.x + 120
    const upstreamCenterX = upstream.position.x + 80
    const consumerTopY = sports.position.y + consumer.position.y

    expect(Math.abs(upstreamCenterX - consumerCenterX)).toBeLessThan(1)
    expect(upstream.position.y + 56).toBeLessThan(consumerTopY)
  })
})
