import { describe, it, expect } from 'vitest'
import { snapshotOp } from './opProgress.svelte'
import type { ResourceStatus } from './types.gen'

function svc(name: string, kind: 'service' | 'container', state: string, reason?: string): ResourceStatus {
  return { name, kind, state, state_reason: reason, restart_count: 0 } as ResourceStatus
}

describe('snapshotOp', () => {
  it('up: in flight while anything is starting/building/pending', () => {
    const snap = snapshotOp('up', [
      svc('redis', 'container', 'healthy'),
      svc('worker', 'service', 'building'),
      svc('payments', 'service', 'pending'),
    ])
    expect(snap.done).toBe(false)
    expect(snap.inFlight).toEqual(['worker', 'payments'])
  })

  it('up: done with degraded summary carrying reasons', () => {
    const snap = snapshotOp('up', [
      svc('redis', 'container', 'healthy'),
      svc('worker', 'service', 'degraded', 'exited: exit status 2'),
    ])
    expect(snap.done).toBe(true)
    expect(snap.healthy).toBe(1)
    expect(snap.degraded).toEqual([{ name: 'worker', reason: 'exited: exit status 2' }])
  })

  it('infra: only containers count', () => {
    const snap = snapshotOp('infra', [
      svc('redis', 'container', 'starting'),
      svc('worker', 'service', 'pending'), // ignored — not a container
    ])
    expect(snap.inFlight).toEqual(['redis'])
  })

  it('down: done when nothing is running', () => {
    expect(snapshotOp('down', [svc('redis', 'container', 'healthy')]).done).toBe(false)
    expect(snapshotOp('down', [svc('redis', 'container', 'stopped')]).done).toBe(true)
  })
})
