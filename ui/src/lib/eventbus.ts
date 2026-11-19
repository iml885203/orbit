// Single SSE connection to /api/events that dispatches named events to
// per-type subscribers. Replaces the previous one-EventSource-per-feature
// approach, which saturated the HTTP/1.1 6-per-origin connection cap once
// the dashboard mounted ~6 streams.

type Handler = (data: unknown) => void

const listeners = new Map<string, Map<Handler, number>>()
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
  const connection = source
  connection.addEventListener(type, (e) => {
    if (source !== connection) return
    const ls = listeners.get(type)
    if (!ls || !ls.size) return
    let parsed: unknown
    try {
      parsed = JSON.parse((e as MessageEvent).data)
    } catch (err) {
      console.error('[eventbus] parse failed', type, err)
      return
    }
    for (const fn of ls.keys()) fn(parsed)
  })
}

function connect() {
  clearTimeout(reconnectTimer)
  reconnectTimer = undefined
  if (source || refCount === 0) return
  const connection = new EventSource('/api/events')
  source = connection
  attached.clear()
  connection.onerror = () => {
    if (source !== connection) return
    connection.close()
    source = null
    attached.clear()
    for (const fn of disconnectListeners) fn()
    if (refCount > 0 && !source) reconnectTimer = setTimeout(connect, 3000)
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
    ls = new Map()
    listeners.set(type, ls)
  }
  ls.set(handler, (ls.get(handler) ?? 0) + 1)
  refCount++
  if (!source) {
    connect()
  } else {
    attach(type)
  }
  let subscribed = true
  return () => {
    if (!subscribed) return
    subscribed = false
    const set = listeners.get(type)
    if (!set || !set.has(handler)) return
    const count = set.get(handler)!
    if (count === 1) set.delete(handler)
    else set.set(handler, count - 1)
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
