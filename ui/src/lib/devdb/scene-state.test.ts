import { describe, expect, it } from 'vitest'
import type { DBOpInFlight } from './stores.svelte'
import { deriveSceneState, publishProgressPercent } from './scene-state'

function publishOp(overrides: Partial<DBOpInFlight> = {}): DBOpInFlight {
  return { op: 'publish', db: 'AccountDB', startedAt: '2026-07-19T00:00:00Z', lines: [], done: false, ok: false, ...overrides }
}

describe('deriveSceneState', () => {
  it('is idle without a selected project', () => expect(deriveSceneState(null, true, false)).toBe('idle'))
  it('is ready when a project is selected and SQL Server is healthy', () => expect(deriveSceneState(null, true, true)).toBe('ready'))
  it('is idle when SQL Server is unavailable', () => expect(deriveSceneState(null, false, true)).toBe('idle'))
  it('is building while publish is running', () => expect(deriveSceneState(publishOp(), true, true)).toBe('building'))
  it('reflects a successful publish', () => expect(deriveSceneState(publishOp({ done: true, ok: true }), true, true)).toBe('complete'))
  it('reflects a failed publish', () => expect(deriveSceneState(publishOp({ done: true }), true, true)).toBe('failed'))
})

describe('publishProgressPercent', () => {
  it('starts at zero and approaches 100 without reaching it while running', () => {
    const op = publishOp()
    expect(publishProgressPercent(op, 0)).toBe(0)
    expect(publishProgressPercent(op, 8)).toBeCloseTo(63.21, 1)
    expect(publishProgressPercent(op, 120)).toBeLessThan(100)
  })

  it('snaps successful completion to 100 and all other settled states to zero', () => {
    expect(publishProgressPercent(publishOp({ done: true, ok: true }), 2)).toBe(100)
    expect(publishProgressPercent(publishOp({ done: true }), 20)).toBe(0)
    expect(publishProgressPercent(null, 20)).toBe(0)
  })
})
