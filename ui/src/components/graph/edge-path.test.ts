import { Position } from '@xyflow/svelte'
import { describe, expect, it, vi } from 'vitest'
import { buildDependencyPath } from './edge-path'

vi.mock('@xyflow/svelte', () => ({
  Position: { Top: 'top', Bottom: 'bottom' },
  getBezierPath: ({ sourceX, sourceY, targetX, targetY }: {
    sourceX: number; sourceY: number; targetX: number; targetY: number
  }) => [`M${sourceX},${sourceY} L${targetX},${targetY}`, (sourceX + targetX) / 2, (sourceY + targetY) / 2],
}))

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

  it('attaches routed edges to their assigned source and target ports', () => {
    const left = buildDependencyPath({ ...verticalEdge, async: false, sourceOffset: -48, targetOffset: -24 })
    const right = buildDependencyPath({ ...verticalEdge, async: false, sourceOffset: 48, targetOffset: 24 })

    expect(left.path).toBe('M52,100 L76,360')
    expect(right.path).toBe('M148,100 L124,360')
  })
})
