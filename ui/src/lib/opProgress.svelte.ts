// Progress state for global lifecycle operations started from the dashboard
// (Up All / Infra Only / Down). The daemon queues the work and returns
// immediately; actual progress is derived from the same SSE status feed the
// graph renders, so the banner needs no new endpoint — it watches service
// states until the operation reaches a terminal shape.

import type { ResourceStatus } from './types.gen'

export type OpKind = 'up' | 'infra' | 'down'

export type OpSnapshot = {
  // Services still moving toward the target state.
  inFlight: string[]
  // Degraded services (with reasons when known) — the failure summary.
  degraded: { name: string; reason?: string }[]
  healthy: number
  done: boolean
}

// snapshotOp reduces the live service map to what the banner shows. Pure so
// the completion rules are unit-testable:
//   up    — done when nothing is starting/building/pending
//   infra — same, but only containers count
//   down  — done when nothing is running at all
export function snapshotOp(kind: OpKind, services: ResourceStatus[]): OpSnapshot {
  const scoped = kind === 'infra' ? services.filter((s) => s.kind === 'container') : services
  const moving = (s: ResourceStatus) => ['starting', 'building', 'pending'].includes(s.state)
  const running = (s: ResourceStatus) => !['stopped', 'pending'].includes(s.state)

  if (kind === 'down') {
    const inFlight = scoped.filter(running).map((s) => s.name)
    return { inFlight, degraded: [], healthy: 0, done: inFlight.length === 0 }
  }
  const inFlight = scoped.filter(moving).map((s) => s.name)
  return {
    inFlight,
    degraded: scoped
      .filter((s) => s.state === 'degraded')
      .map((s) => ({ name: s.name, reason: s.state_reason })),
    healthy: scoped.filter((s) => s.state === 'healthy').length,
    done: inFlight.length === 0,
  }
}

class OpProgress {
  active = $state<{ kind: OpKind; startedAt: number } | null>(null)

  start(kind: OpKind) {
    this.active = { kind, startedAt: Date.now() }
  }

  clear() {
    this.active = null
  }
}

export const opProgress = new OpProgress()
