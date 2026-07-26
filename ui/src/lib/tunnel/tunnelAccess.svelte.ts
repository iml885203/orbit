import { subscribe } from '$lib/eventbus'
import type { AccessLine } from './api'

const CAP = 200

// recent access lines keyed by local port. $state so the dashboard reacts.
export const tunnelAccess = $state<{ byPort: Record<number, AccessLine[]> }>({ byPort: {} })

// lineKey identifies an access line for dedupe. The server replays its whole
// retained ring on every (re)connect (SSE reconnect after a daemon restart or
// network blip), so without this the same lines re-append on each reconnect —
// inflating the list and colliding the {#each} key in TunnelAccessLog. Same
// fields as that key, so a dedupe here keeps the rendered key unique too.
function lineKey(l: AccessLine): string {
  return `${l.time}|${l.method}|${l.path}|${l.status}|${l.duration_ms}`
}

// startTunnelAccessStream subscribes to the SSE `tunnel-access` events and keeps
// a capped, newest-last, deduped list per local port. Returns an unsubscribe fn.
export function startTunnelAccessStream(): () => void {
  return subscribe('tunnel-access', (data) => {
    const l = data as AccessLine
    const cur = tunnelAccess.byPort[l.local_port] ?? []
    const key = lineKey(l)
    if (cur.some((p) => lineKey(p) === key)) return // replayed line already present
    const next = [...cur, l]
    tunnelAccess.byPort[l.local_port] = next.length > CAP ? next.slice(next.length - CAP) : next
  })
}
