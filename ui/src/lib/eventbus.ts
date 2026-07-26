// Single SSE connection to /api/events that dispatches named events to
// per-type subscribers. Replaces the previous one-EventSource-per-feature
// approach, which saturated the HTTP/1.1 6-per-origin connection cap once
// the dashboard mounted ~6 streams.

type Handler = (data: unknown) => void

const listeners = new Map<string, Set<Handler>>()
// attached tracks which event types currently have a live EventSource listener
// bound. A type can re-enter listeners after going to zero subscribers, and
// without this guard each re-entry would addEventListener again — multiplying
// dispatch on every subsequent frame.
const attached = new Set<string>()
const disconnectListeners = new Set<() => void>()
let source: EventSource | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | undefined
let refCount = 0

function attach(type: string) {
  if (!source || attached.has(type)) return
  attached.add(type)
  source.addEventListener(type, (e) => {
    const ls = listeners.get(type)
    if (!ls || !ls.size) return
    let parsed: unknown
    try {
      parsed = JSON.parse((e as MessageEvent).data)
    } catch (err) {
      console.error('[eventbus] parse failed', type, err)
      return
    }
    for (const fn of ls) fn(parsed)
  })
}

function connect() {
  source = new EventSource('/api/events')
  attached.clear()
  source.onerror = () => {
    source?.close()
    source = null
    attached.clear()
    for (const fn of disconnectListeners) fn()
    if (refCount > 0) reconnectTimer = setTimeout(connect, 3000)
  }
  for (const type of listeners.keys()) attach(type)
}

/**
 * Subscribe to a single event type on the shared /api/events stream.
 * The connection is opened on the first subscribe and closed on the last
 * unsubscribe. Returns the unsubscribe function.
 */
export function subscribe(type: string, handler: Handler): () => void {
  let ls = listeners.get(type)
  if (!ls) {
    ls = new Set()
    listeners.set(type, ls)
  }
  ls.add(handler)
  refCount++
  if (!source) {
    connect()
  } else {
    attach(type)
  }
  return () => {
    const set = listeners.get(type)
    if (!set || !set.has(handler)) return
    set.delete(handler)
    if (!set.size) listeners.delete(type)
    refCount--
    if (refCount === 0) {
      source?.close()
      source = null
      attached.clear()
      clearTimeout(reconnectTimer)
      reconnectTimer = undefined
    }
  }
}

/**
 * Register a handler invoked whenever the shared connection drops (network
 * error, daemon restart). Returns the unregister function. Multiple handlers
 * may be registered; each fires on disconnect.
 */
export function onConnectionLost(handler: () => void): () => void {
  disconnectListeners.add(handler)
  return () => { disconnectListeners.delete(handler) }
}
