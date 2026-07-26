// Trace-list filter ↔ URL query serialization. Lives in a plain .ts module
// (not the .svelte page) so it can use URLSearchParams — eslint's
// svelte/prefer-svelte-reactivity rule flags built-in mutable collections
// inside .svelte files, but these are transient parse/build helpers with no
// reactive state.

export type TraceListFilters = {
  errored: boolean
  minDurationMs: number
  search: string
}

export function parseTraceQuery(qs: string): TraceListFilters {
  const p = new URLSearchParams(qs)
  return {
    errored: p.get('errored') === '1',
    minDurationMs: Number(p.get('min') ?? 0) || 0,
    search: p.get('q') ?? '',
  }
}

// buildTraceQuery returns '' when every filter is at its default, so the URL
// stays clean until the user actually filters.
export function buildTraceQuery(f: TraceListFilters): string {
  const p = new URLSearchParams()
  if (f.errored) p.set('errored', '1')
  if (f.minDurationMs > 0) p.set('min', String(f.minDurationMs))
  if (f.search.trim()) p.set('q', f.search.trim())
  return p.toString()
}
