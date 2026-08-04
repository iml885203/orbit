import type { APIResponse, DoctorResponse, Settings, EnvToggleInfo, VersionResponse, EnvsResponse, EnvironmentSwitchResponse, GraphResponse, LogsResponse, ServiceEnvResponse } from './types.gen'
import type { HistoryRecord, HistoryFilter } from './history.svelte'
import { toast } from './stores.svelte'

// getJSON wraps daemon GET endpoints so failures are never silent (per
// svelte-error-surface): every failure logs, and user-initiated fetches pass
// an `unavailable` message that surfaces as a toast. Polled/startup fetches
// pass none — the header's connection dot owns daemon-down signalling, and a
// toast per poll tick would spam.
// Exported: the extension module's api calls build on the same
// toast-aware GET primitive.
export async function getJSON<T>(path: string, unavailable?: string, signal?: AbortSignal): Promise<T | null> {
  try {
    const res = await fetch(path, { signal })
    if (res.ok) return await res.json()
    console.error(`GET ${path}: HTTP ${res.status}`)
  } catch (e) {
    console.error(`GET ${path}:`, e)
  }
  if (unavailable) toast(unavailable)
  return null
}

export async function apiPost(path: string, body: Record<string, unknown> = {}): Promise<{ ok: boolean; data?: APIResponse }> {
  try {
    const res = await fetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    const data: APIResponse = await res.json()
    return { ok: res.ok && !data.error, data }
  } catch (e) {
    console.error(`POST ${path}:`, e)
    return { ok: false, data: { error: (e as Error).message } }
  }
}

export async function apiPut<T extends APIResponse = APIResponse>(path: string, body: Record<string, unknown> = {}): Promise<{ ok: boolean; data?: T }> {
  try {
    const res = await fetch(path, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    const data: T = await res.json()
    return { ok: res.ok && !data.error, data }
  } catch (e) {
    console.error(`PUT ${path}:`, e)
    return { ok: false, data: { error: (e as Error).message } as T }
  }
}

export async function fetchDoctor(): Promise<DoctorResponse | null> {
  return getJSON('/api/doctor', 'health checks unavailable')
}

export async function fetchSettings(): Promise<Settings> {
  return (await getJSON<Settings>('/api/settings')) ?? {}
}

export async function fetchEnvToggles(): Promise<EnvToggleInfo[]> {
  return (await getJSON<EnvToggleInfo[]>('/api/env-toggles')) ?? []
}

export async function setEnvToggle(service: string, varName: string, enabled: boolean): Promise<{ ok: boolean; data?: APIResponse }> {
  return apiPut('/api/env-toggles', { service, var: varName, enabled })
}

export async function fetchVersion(signal?: AbortSignal): Promise<VersionResponse | null> {
  return getJSON('/api/version', undefined, signal)
}

export async function restartForUpdate(): Promise<{ ok: boolean; data?: APIResponse }> {
  return apiPost('/api/version/restart')
}

export async function fetchEnvs(): Promise<EnvsResponse | null> {
  return getJSON('/api/envs')
}

export async function mutateSource(action: Record<string, unknown>) {
	return apiPost('/api/sources', action)
}

// Polled every status tick — log-only on failure (connection dot covers it).
export async function fetchGraph(env?: string): Promise<GraphResponse | null> {
  const url = env ? `/api/graph?env=${encodeURIComponent(env)}` : '/api/graph'
  return getJSON(url)
}

export async function fetchLogs(name: string): Promise<string[] | null> {
  const response = await getJSON<LogsResponse>(
    `/api/logs/${encodeURIComponent(name)}?lines=1000`,
    'logs unavailable',
  )
  return response?.lines ?? null
}

export async function fetchServiceEnv(name: string): Promise<ServiceEnvResponse | null> {
  return getJSON(`/api/service-env/${encodeURIComponent(name)}`, 'service env unavailable')
}

export async function detachEdge(env: string, from: string, to: string, detached: boolean) {
  return apiPut(`/api/edges/${encodeURIComponent(from)}/${encodeURIComponent(to)}`, { env, detached })
}

export async function switchEnv(env: string, confirmed = false, currentIdentity = '', targetIdentity = '', runningResources: string[] = []) {
  return apiPut<EnvironmentSwitchResponse>('/api/envs/current', {
    env,
    confirmed,
    current_identity: currentIdentity,
    target_identity: targetIdentity,
		running_resources: runningResources,
  })
}

export async function fetchHistoryList(filter: HistoryFilter = {}): Promise<HistoryRecord[]> {
  const params = new URLSearchParams()
  if (filter.source) params.set('source', filter.source)
  if (filter.onlyNoCli) params.set('onlyNoCli', 'true')
  if (filter.onlyErrors) params.set('onlyErrors', 'true')
  if (filter.limit) params.set('limit', String(filter.limit))
  return (await getJSON<HistoryRecord[]>(`/api/history/list?${params.toString()}`, 'history unavailable')) ?? []
}
