import { describe, expect, it } from 'vitest'
import type { DBOpInFlight } from './stores.svelte'
import { publishProgressPercent } from './scene-state'

function publishOp(overrides: Partial<DBOpInFlight> = {}): DBOpInFlight {
  return { op: 'publish', db: 'AccountDB', startedAt: '2026-07-19T00:00:00Z', lines: [], done: false, ok: false, ...overrides }
}

describe('publishProgressPercent', () => {
  it('starts at zero and approaches 100 without reaching it while running', () => {
    const op = publishOp()
    expect(publishProgressPercent(op, 0)).toBe(0)
    expect(publishProgressPercent(op, 8)).toBeGreaterThan(0)
    expect(publishProgressPercent(op, 120)).toBeLessThan(100)
  })

  it('snaps successful completion to 100 and all other settled states to zero', () => {
    expect(publishProgressPercent(publishOp({ done: true, ok: true }), 2)).toBe(100)
    expect(publishProgressPercent(publishOp({ done: true }), 20)).toBe(0)
    expect(publishProgressPercent(null, 20)).toBe(0)
  })
})
