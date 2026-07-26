// Drift state for the DB dashboard: the last schema-diff summary per
// database, shared across every project's rows so a single Check-all sweep
// can populate them all at once. Lives above the project list (DevDBPage)
// and is threaded down as props.
//
// A summary persists after its diff until the same database's publish/reset
// invalidates it — then it's marked `stale` (shown muted, "recheck") rather
// than dropped, so the user still sees the last-known drift while knowing it
// may be out of date.
import type { DiffResult, DriftEntry } from '$lib/types.gen'
import { SvelteSet } from 'svelte/reactivity'
import { diffDB, fetchDriftSnapshot } from './api'

// DriftSummary is the on-demand result of the last `db diff` for one
// database: how far the project is ahead of the live DB. Owned here (the
// drift domain module) because it's the core type of this controller and
// has 5+ consumers — DatabaseRow, DBProjectList, DevDBWorkspace,
// DatabaseOperationsList, and the tests all import it (svelte-types.md:
// extract past the 3-consumer threshold). `stale` marks a summary a
// publish/reset has invalidated; `error` records a per-DB diff failure.
export type DriftSummary = { inSync: boolean; changes: number; dataLoss: boolean; stale?: boolean; needsEngine?: boolean; error?: string }

function summarize(r: DiffResult): DriftSummary {
  // A file-level fast diff carries no ops — its badge counts changed
  // source files instead of engine operations.
  const opCount = r.created + r.altered + r.dropped
  return { inSync: r.in_sync, changes: opCount || (r.file_changes?.length ?? 0), dataLoss: r.data_loss }
}

// ProjectDrift is the rolled-up drift badge for a whole project: the
// aggregate of its databases' summaries. Undefined when none were diffed.
export type ProjectDrift =
  | { kind: 'in-sync' }
  | { kind: 'changes'; changes: number; dataLoss: boolean; stale: boolean }
  | { kind: 'unchecked' }
  | { kind: 'error' }

// summarizeProject folds a project's per-DB drift into one badge. A single
// diff error dominates (something is wrong); otherwise sum the changes and
// carry data-loss/stale flags. Returns undefined when no DB was diffed yet.
export function summarizeProject(databases: string[], byDB: Record<string, DriftSummary>): ProjectDrift | undefined {
  let seen = false
  let anyError = false
  let changes = 0
  let dataLoss = false
  let stale = false
  let allInSync = true
  let needsEngine = false
  for (const db of databases) {
    const s = byDB[db]
    if (!s) continue
    seen = true
    if (s.error) { anyError = true; continue }
    if (s.needsEngine) { needsEngine = true; allInSync = false; continue }
    if (!s.inSync) { allInSync = false; changes += s.changes }
    if (s.dataLoss) dataLoss = true
    if (s.stale) stale = true
  }
  if (!seen) return undefined
  if (anyError) return { kind: 'error' }
  if (needsEngine && changes === 0) return { kind: 'unchecked' }
  if (allInSync && changes === 0) return { kind: 'in-sync' }
  return { kind: 'changes', changes, dataLoss, stale }
}

// A first-time refresh may need to build several projects; the same bound
// keeps automatic and manual sweeps from competing for the whole host.
const checkAllConcurrency = 4

export function createDriftController() {
  let byDB = $state<Record<string, DriftSummary>>({})
  // SvelteSet is reactive under mutation, so add/delete propagate without a
  // reference swap.
  const diffing = new SvelteSet<string>()
  let checkingAll = $state(false)
  let checkDone = $state(0)
  let checkTotal = $state(0)

  // load seeds the badges from the daemon's drift cache (each DB's last
  // diff outcome, stale-flagged when a publish/reset landed since), so a
  // page reload restores what the last check found instead of starting
  // blank. Entries never overwrite a badge a live diff already set this
  // session, and a failed fetch quietly leaves rows "not checked yet".
  async function load(): Promise<void> {
    const snap = await fetchDriftSnapshot()
    if (!snap?.entries) return
    const next = { ...byDB }
    for (const e of snap.entries as DriftEntry[]) {
      if (next[e.db] || diffing.has(e.db)) continue
      next[e.db] = e.result
        ? { ...summarize(e.result), ...(e.stale ? { stale: true } : {}) }
        : { inSync: false, changes: 0, dataLoss: false, error: e.error || 'diff failed', ...(e.stale ? { stale: true } : {}) }
    }
    byDB = next
  }

  // One worker serves automatic and manual refreshes so badge/error handling
  // stays identical.
  async function refreshDrift(db: string, mode: 'fast' | 'normal' | 'analyze' = 'normal'): Promise<void> {
    diffing.add(db)
    try {
      const { ok, data } = await diffDB(db, false, mode)
      if (ok && data?.result) {
        byDB = { ...byDB, [db]: summarize(data.result) }
      } else if (ok && data?.needs_engine) {
        byDB = { ...byDB, [db]: { inSync: false, changes: 0, dataLoss: false, needsEngine: true } }
      } else if (!ok) {
        byDB = { ...byDB, [db]: { inSync: false, changes: 0, dataLoss: false, error: data?.error ?? 'diff failed' } }
      }
    } finally {
      diffing.delete(db)
    }
  }

  // The bounded pool protects the host when a first-time refresh needs the
  // engine; routine indexed refreshes share it so progress stays consistent.
  async function sweep(dbs: string[], mode: 'fast' | 'normal' | 'analyze', retain = dbs): Promise<void> {
    if (checkingAll || dbs.length === 0) return
    checkingAll = true
    checkDone = 0
    checkTotal = dbs.length
    try {
      const queue = [...dbs]
      const worker = async () => {
        for (let db = queue.shift(); db !== undefined; db = queue.shift()) {
          await refreshDrift(db, mode)
          checkDone++
        }
      }
      await Promise.all(Array.from({ length: Math.min(checkAllConcurrency, queue.length) }, worker))
      byDB = Object.fromEntries(Object.entries(byDB).filter(([db]) => retain.includes(db)))
    } finally {
      checkingAll = false
    }
  }

  async function checkFast(dbs: string[]): Promise<void> {
    await sweep(dbs, 'fast')
  }

  // Page entry is progressive: known databases return through the fast
  // fingerprint/cache path first, then only first-time databases pay for an
  // engine diff in the background. Once established, future entries stay on
  // the fast path without asking the user to understand cache warm-up.
  async function checkOnEntry(dbs: string[]): Promise<void> {
    await sweep(dbs, 'fast')
    const firstChecks = dbs.filter((db) => byDB[db]?.needsEngine)
    if (firstChecks.length > 0) await sweep(firstChecks, 'normal', dbs)
  }

  async function checkAll(dbs: string[]): Promise<void> {
    await sweep(dbs, 'normal')
  }

  function recordResult(db: string, result: DiffResult): void {
    byDB = { ...byDB, [db]: summarize(result) }
  }

  return {
    get byDB() { return byDB },
    get diffing() { return diffing },
    get checkingAll() { return checkingAll },
    // checkProgress is the live sweep counter for the header button
    // ("Checking 3/8…"); total is 0 when no sweep has run yet.
    get checkProgress() { return { done: checkDone, total: checkTotal } },
    load,
    refreshDrift,
    checkFast,
    checkOnEntry,
    checkAll,
    recordResult,
  }
}

export type DriftController = ReturnType<typeof createDriftController>
