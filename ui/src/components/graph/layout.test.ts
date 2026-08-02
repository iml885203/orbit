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
