// DB-dashboard API calls: the local SQL Server publish/reset workflow.
// Built on the core getJSON/apiPost primitives; the store-update contracts
// live here with their funcs.
import { getJSON, apiPost } from '$lib/api'
import type {
  APIResponse,
  DBDiffResponse,
  DBResetState,
  DBResetStateResponse,
  DevDBMetaResponse,
  DriftSnapshotResponse,
  DevDBProject,
  Settings,
  Snapshot as DBStateSnapshot,
} from '$lib/types.gen'
import { devStore } from './stores.svelte'

// quiet: polled/env-switch refetches suppress the toast so a transient
// failure during a daemon reload doesn't spam; user-initiated fetches keep it.
// Not exported: consumers go through refreshDevMeta so the store-update
// contract below can't be bypassed.
async function fetchDevMeta({ quiet = false } = {}): Promise<DevDBMetaResponse | null> {
  return getJSON('/api/devdb/meta', quiet ? undefined : 'devdb metadata unavailable')
}

// refreshDevMeta owns the store-update contract for devMeta: a null response
// (fetch failed) keeps the previous value, because dbWorkflowHidden() reads
// this and a null overwrite would flash the SQL Server tab out.
export async function refreshDevMeta(opts: { quiet?: boolean } = {}): Promise<void> {
  const meta = await fetchDevMeta(opts)
  if (meta) devStore.devMeta = meta
}

// Returns null on fetch failure (so callers can distinguish "request
// failed" from "genuinely no projects"); the array (possibly empty) on
// success.
export async function fetchDBProjects(): Promise<DevDBProject[] | null> {
  const data = await getJSON<{ projects?: DevDBProject[] }>('/api/devdb/projects', 'db projects unavailable')
  return data ? data.projects ?? [] : null
}

// force allows destructive changes (BlockOnPossibleDataLoss=false) — sent
// only from the explicit "Publish anyway" confirmation after a publish was
// blocked on possible data loss.
export async function publishDB(db: string, force = false): Promise<{ ok: boolean; data?: APIResponse }> {
  return apiPost('/api/db/publish', { db, force })
}

export async function resetDB(db: string): Promise<{ ok: boolean; data?: APIResponse }> {
  return apiPost('/api/db/reset', { db, acknowledgeDataLoss: true })
}

export type DiffMode = 'fast' | 'normal' | 'analyze'

// fast never starts the engine; analyze always does. Keeping that decision at
// the call site prevents page-entry refreshes from turning into surprise
// multi-second builds while explicit checks can still promise exact ops.
export async function diffDB(
  db: string,
  script = false,
  mode: DiffMode = 'normal',
): Promise<{ ok: boolean; data?: DBDiffResponse & APIResponse }> {
  return apiPost('/api/db/diff', {
    db,
    script,
    ...(mode === 'fast' ? { fast_only: true } : {}),
    ...(mode === 'analyze' ? { analyze: true } : {}),
  })
}

// fetchDriftSnapshot reads the daemon's drift cache — each database's
// last diff outcome — so a fresh page load restores its badges. Quiet +
// null-on-failure: badges degrade to "not checked yet".
export async function fetchDriftSnapshot(): Promise<DriftSnapshotResponse | null> {
  return getJSON<DriftSnapshotResponse>('/api/db/drift')
}

// fetchResetState live-probes each database's reset readiness (exists +
// standard/recreate). Quiet + null-on-failure: it drives the pre-click
// legacy notice and reset-disabled state, both of which safely degrade
// to "unknown" (the POST /api/db/reset 409 gate still protects the run).
export type ResetStateMap = Record<string, DBResetState>
export async function fetchResetState(): Promise<ResetStateMap | null> {
  const data = await getJSON<DBResetStateResponse>('/api/db/reset-state')
  return data ? data.states ?? {} : null
}

// publishAllDBs publishes every database sequentially under the one op
// lock; the daemon stops at the first failure.
export async function publishAllDBs(): Promise<{ ok: boolean; data?: APIResponse }> {
  return apiPost('/api/db/publish', { all: true })
}

export async function fetchDBState(): Promise<DBStateSnapshot | null> {
  return getJSON('/api/db-state')
}

export type SettingsWire = Settings
