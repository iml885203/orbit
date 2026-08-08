// Tunnel-dashboard API calls: the local devproxy tunnel list and the
// gateway-wide claim view. Built on the core request primitives.
import { apiPost, getJSON } from '$lib/api'
import type { APIResponse, AccessLine, ClaimAPIRequest, GlobalClaimView, TunnelListResponse, TunnelView } from '$lib/types.gen'

export type { AccessLine, TunnelView }
export type GlobalClaim = GlobalClaimView
export type TunnelList = TunnelListResponse

// Returns null on fetch failure (so callers can distinguish "request failed"
// from "genuinely no tunnels"). No `unavailable` message: this backs a 2s
// poll, and per getJSON's contract a toast per tick would spam — worse, the
// 2s cadence outruns the 2.5s auto-hide, so the toast would never clear.
export async function fetchTunnels(): Promise<TunnelList | null> {
  return getJSON<TunnelList>('/api/tunnel')
}

export async function claimTunnel(localPort: number, path: string): Promise<{ ok: boolean; data?: APIResponse }> {
  const request: ClaimAPIRequest = { local_port: localPort, paths: [path] }
  return apiPost('/api/tunnel/claim', { ...request })
}

// fetchGlobalClaims lists every claim on the gateway. Unlike the local
// tunnel list this can genuinely fail, so the error is propagated — never
// swallowed into an empty list that would read as "no claims".
export async function fetchGlobalClaims(): Promise<GlobalClaim[]> {
  const res = await fetch('/api/tunnel?all=true')
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error || `request failed (HTTP ${res.status})`)
  }
  const data = await res.json()
  return data.claims ?? []
}
