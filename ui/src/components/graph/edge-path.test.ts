import { Position } from '@xyflow/svelte'
import { describe, expect, it } from 'vitest'
import { buildDependencyPath } from './edge-path'

const verticalEdge = {
  id: 'odds-api->settlement-worker:accounts.settlement',
  sourceX: 100,
  sourceY: 100,
  sourcePosition: Position.Bottom,
  targetX: 100,
  targetY: 360,
  targetPosition: Position.Top,
}

describe('buildDependencyPath', () => {
  it('bows async edges away from sync edges with the same endpoints', () => {
    const sync = buildDependencyPath({ ...verticalEdge, async: false })
    const async = buildDependencyPath({ ...verticalEdge, async: true })

    expect(async.path).not.toBe(sync.path)
    expect(Math.abs(async.labelX - sync.labelX)).toBeGreaterThanOrEqual(20)
  })

  it('routes fanned sync edges through straight side lanes', () => {
    const left = buildDependencyPath({ ...verticalEdge, async: false, routeLane: -1 })
    const right = buildDependencyPath({ ...verticalEdge, async: false, routeLane: 1 })

    expect(left.path).not.toBe(right.path)
    expect(Math.abs(left.labelX - verticalEdge.sourceX)).toBeGreaterThan(120)
    expect(Math.abs(right.labelX - verticalEdge.sourceX)).toBeGreaterThan(120)
    expect(left.path).toContain(`L${left.labelX},`)
    expect(right.path).toContain(`L${right.labelX},`)
  })
})
