// Tracing domain state for the dashboard: the live trace list + collector
// status, fed by the 'trace' event on the shared /api/events SSE stream, plus
// the fetch helpers the Tracing pages use.
//
// The list is kept newest-first and capped; a 'trace' event upserts a summary
// (a trace accumulates spans over time, so the same id can update repeatedly).

import type { Trace, TraceSummary, TracingStatus, TraceLogsResponse } from './types.gen'
import { subscribe } from './eventbus'
import { toast } from './stores.svelte'

const MAX_LIST = 500

class TracingStore {
  traces = $state<TraceSummary[]>([])
  status = $state<TracingStatus | null>(null)
  // True when the last status poll failed — the page shows a targeted
  // "status unavailable" state instead of silently hiding the indicator.
  statusUnavailable = $state(false)

  get navigationVisible() {
    if (this.traces.length > 0) return true
    if (!this.status?.configured) return false
    return !this.status.receiverHealthy || this.status.traceCount > 0
  }

  upsert(sum: TraceSummary) {
    const i = this.traces.findIndex((t) => t.traceId === sum.traceId)
    if (i >= 0) {
      // Common case: an existing trace gains spans but keeps its start time —
      // update in place, no re-sort. Only re-sort if its start actually moved.
      const moved = this.traces[i].startUnixNano !== sum.startUnixNano
      this.traces[i] = sum
      if (moved) this.traces.sort((a, b) => b.startUnixNano - a.startUnixNano)
    } else {
      this.traces.unshift(sum)
      if (this.traces.length > MAX_LIST) this.traces.length = MAX_LIST
      this.traces.sort((a, b) => b.startUnixNano - a.startUnixNano)
    }
  }

  reset() {
    this.traces = []
    this.status = null
    this.statusUnavailable = false
  }
}

export const tracing = new TracingStore()

// subscribeLiveTraces wires the SSE 'trace' event into the store. Call from a
// page's onMount and run the returned cleanup on unmount.
export function subscribeLiveTraces(): () => void {
  return subscribe('trace', (data) => {
    tracing.upsert(data as TraceSummary)
  })
}

export async function fetchTraces(limit = 200): Promise<TraceSummary[]> {
  try {
    const res = await fetch(`/api/traces?limit=${limit}`)
    if (res.ok) return (await res.json()) ?? []
    toast('trace list unavailable')
  } catch (e) {
    toast('trace list unavailable')
    console.error('fetchTraces:', e)
  }
  return []
}

// fetchTrace returns one full trace, or null when unknown/evicted (404) or on
// a transport error. The caller distinguishes "expired" via the boolean in the
// result so the detail page can show the right empty state; transport errors
// are surfaced via toast (per svelte-error-surface).
export async function fetchTrace(traceId: string): Promise<{ trace: Trace | null; gone: boolean }> {
  try {
    const res = await fetch(`/api/traces/${encodeURIComponent(traceId)}`)
    if (res.ok) return { trace: await res.json(), gone: false }
    if (res.status === 404) return { trace: null, gone: true }
    toast('trace unavailable')
  } catch (e) {
    toast('trace unavailable')
    console.error('fetchTrace:', e)
  }
  return { trace: null, gone: false }
}

// fetchTracingStatus updates tracing.statusUnavailable instead of toasting:
// it runs on a 3s poll, and a toast per failed tick would spam while the
// daemon restarts. The page renders the flag as a targeted error state.
export async function fetchTracingStatus(): Promise<TracingStatus | null> {
  try {
    const res = await fetch('/api/tracing/status')
    if (res.ok) {
      tracing.statusUnavailable = false
      return await res.json()
    }
  } catch (e) {
    console.error('fetchTracingStatus:', e)
  }
  tracing.statusUnavailable = true
  return null
}

// fetchTraceLogs returns the trace's log lines as "[service] line" strings,
// joined server-side by the daemon (GET /api/traces/{id}/logs) — one
// implementation of the exact-id join shared with the CLI. See the daemon's
// traceLogs for the join contract and its log-ring-buffer ceiling.
export async function fetchTraceLogs(traceId: string): Promise<string[]> {
  try {
    const res = await fetch(`/api/traces/${encodeURIComponent(traceId)}/logs`)
    if (res.ok) {
      const data: TraceLogsResponse = await res.json()
      return (data.lines ?? []).map((l) => `[${l.service}] ${l.line}`)
    }
    if (res.status !== 404) toast('trace logs unavailable')
  } catch (e) {
    toast('trace logs unavailable')
    console.error('fetchTraceLogs:', e)
  }
  return []
}
