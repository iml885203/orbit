// Trace playback state for the Services graph. "Play on graph" loads a trace
// here and navigates to the graph; ServiceNode and DependencyEdge read this
// store directly (like they read the graph store) and self-apply dim / active
// / failed styling. Keeping the reads in the leaf components means GraphView's
// node/edge construction is untouched — playback is purely additive and a
// no-op whenever `active` is false.
//
// Membership is computed with plain arrays (not Set/Map): the lists are tiny
// (a handful of services/hops per local trace) and arrays keep this a rune
// module free of reactive-collection lint concerns.

import type { Trace } from './types.gen'

export type PlaybackStep = { from: string; to: string; error: boolean }

class TracePlayback {
  active = $state(false)
  traceId = $state('')
  rootService = $state('')
  // Distinct services in entry order; the full set the trace touched.
  services = $state<string[]>([])
  // Cross-service hops in span-start order; one per playback step.
  steps = $state<PlaybackStep[]>([])
  failed = $state<string[]>([])
  // Index of the last revealed step. -1 = only the root is lit; the initial
  // state reveals the whole path (steps.length - 1) so the route shows at once.
  current = $state(-1)

  start(trace: Trace) {
    const serviceOf: Record<string, string> = {}
    for (const s of trace.spans) {
      serviceOf[s.spanId] = s.service
    }
    const ordered = [...trace.spans].sort((a, b) => a.startUnixNano - b.startUnixNano)

    const steps: PlaybackStep[] = []
    const failed: string[] = []
    for (const span of ordered) {
      if (span.status === 'error' && !failed.includes(span.service)) failed.push(span.service)
      const parentSvc = span.parentId ? serviceOf[span.parentId] : undefined
      if (parentSvc && parentSvc !== span.service) {
        steps.push({ from: parentSvc, to: span.service, error: span.status === 'error' })
      }
    }

    this.traceId = trace.traceId
    this.rootService = trace.rootService
    // The daemon's summarize() already ships distinct services in first-seen
    // (start-time) order with the root first — reuse the wire field rather
    // than re-deriving it from spans.
    this.services = [...trace.services]
    this.steps = steps
    this.failed = failed
    this.current = steps.length - 1
    this.active = true
  }

  exit() {
    this.active = false
    this.traceId = ''
    this.rootService = ''
    this.services = []
    this.steps = []
    this.failed = []
    this.current = -1
  }

  next() { this.current = Math.min(this.current + 1, this.steps.length - 1) }
  prev() { this.current = Math.max(this.current - 1, -1) }

  // A service is lit when it's the root or an endpoint of a revealed hop.
  // Walks steps[0..current] directly (no slice allocation — called per node
  // and per edge on every render).
  isServiceActive(service: string): boolean {
    if (service === this.rootService) return true
    for (let i = 0; i <= this.current && i < this.steps.length; i++) {
      if (this.steps[i].from === service || this.steps[i].to === service) return true
    }
    return false
  }

  isEdgeActive(from: string, to: string): boolean {
    for (let i = 0; i <= this.current && i < this.steps.length; i++) {
      if (this.steps[i].from === from && this.steps[i].to === to) return true
    }
    return false
  }

  inTrace(service: string): boolean { return this.services.includes(service) }
  serviceFailed(service: string): boolean { return this.failed.includes(service) }
}

export const playback = new TracePlayback()
