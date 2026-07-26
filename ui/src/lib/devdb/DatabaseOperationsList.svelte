<script lang="ts">
  import type { DevDBProject, DBState } from '$lib/types.gen'
  import type { DBOpInFlight } from './stores.svelte'
  import { publishDB, resetDB, type ResetStateMap } from './api'
  import { toast } from '$lib/stores.svelte'
  import ConfirmModal from '$components/ConfirmModal.svelte'
  import LogModal from '$components/LogModal.svelte'
  import DatabaseRow from './DatabaseRow.svelte'
  import type { DriftSummary } from './drift.svelte'
  import { ScrollText } from '@lucide/svelte'
  import { createElapsed, dbOpLabel, dbOpRunning, createSubmitGuard } from './dbOpView.svelte'

  // Drift state (per-DB diff summaries, in-flight set) is owned by the page
  // so a Check-all sweep can populate every project's rows at once; this
  // list only reads it and asks the page to (re)diff one DB via onDiff.
  // onDiff is optional: the page view wires it to the shared drift
  // controller; the compact drawer variant omits it (no drift/check-all
  // there) and the row's Diff button is hidden.
  type Props = { project: DevDBProject; states: Record<string, DBState>; resetStates?: ResetStateMap; operation: DBOpInFlight | null; disabledReason?: string; variant?: 'page' | 'drawer'; driftByDB?: Record<string, DriftSummary>; diffingDBs?: Set<string>; onDiff?: (database: string) => void }
  let { project, states, resetStates = {}, operation, disabledReason = '', variant = 'page', driftByDB = {}, diffingDBs = new Set(), onDiff }: Props = $props()
  let pendingReset = $state<string | null>(null)
  // pendingForce holds the database whose blocked publish awaits the
  // "publish anyway" (data-loss) confirmation.
  let pendingForce = $state<string | null>(null)
  // TODO(reset-display-state): The persistent legacy notice and pre-click missing-DB state require a GET reset-state endpoint; operation timestamps cannot identify either safely.
  let logOpen = $state(false)
  const elapsed = createElapsed(() => operation)
  const allFailed = $derived(!!operation?.all && !!operation.done && !operation.ok)
  const submit = createSubmitGuard(() => dbOpRunning(operation))

  async function publish(database: string, force = false) {
    if (!submit.begin('publish')) return
    toast(force ? `Publishing ${database} (allowing data loss)…` : `Publishing ${database}…`)
    const result = await publishDB(database, force)
    if (!result.ok) { submit.reset(); toast(result.data?.error ?? 'Failed to start publish') }
  }
  function requestPublish(database: string) {
    const drift = driftByDB[database]
    if (drift?.dataLoss && !drift.stale && !drift.error && !drift.needsEngine) {
      pendingForce = database
      return
    }
    void publish(database)
  }
  function confirmForcePublish() {
    if (!pendingForce) return
    const database = pendingForce
    pendingForce = null
    void publish(database, true)
  }
  async function confirmReset() {
    if (!pendingReset || !submit.begin('reset')) return
    const database = pendingReset
    pendingReset = null
    toast(`Resetting ${database}…`)
    const result = await resetDB(database)
    if (result.ok) return
    submit.reset()
    toast(result.data?.error ?? 'Failed to start reset')
  }

  function cancelReset() { pendingReset = null }
</script>

<section class="operations" class:drawer={variant === 'drawer'} aria-label={`${project.name} databases`}>
  {#if variant === 'page'}<header><div><h2>{project.name}</h2><p>{project.path}</p></div><strong>{project.databases.length} {project.databases.length === 1 ? 'database' : 'databases'}</strong></header>{/if}
  {#if project.databases.length === 0}
    <div class="empty"><strong>No databases were found in this project.</strong><p>Check the project path or run <code>orbit env sync</code> to refresh discovery.</p></div>
  {:else}
    {#if allFailed}<div class="all-failed" role="alert"><strong>Publish all stopped at a failure.</strong><span>{operation?.err ?? 'A database failed to publish; the rest were skipped.'}</span><button type="button" onclick={() => logOpen = true}><ScrollText size={13} /> View log</button></div>{/if}
    <div class="rows">{#each project.databases as database (database)}<DatabaseRow {database} state={states[database]} resetState={resetStates[database]} {operation} disabledReason={submit.reason || disabledReason} submittingVerb={submit.pendingVerb} compact={variant === 'drawer'} elapsedSeconds={elapsed.seconds} onPublish={() => requestPublish(database)} onReset={() => { pendingReset = database }} onViewLog={() => logOpen = true} onDiff={onDiff ? () => onDiff(database) : undefined} onForcePublish={() => pendingForce = database} drift={driftByDB[database]} diffing={diffingDBs.has(database)} />{/each}</div>
  {/if}
</section>

{#if pendingForce}<ConfirmModal open title={`Publish ${pendingForce} despite data loss?`} message="The analyzed schema changes may discard data (such as dropped columns or tables). Publishing anyway applies the latest schema and lets that data go. This cannot be undone." confirmLabel="Publish anyway" danger onConfirm={confirmForcePublish} onCancel={() => pendingForce = null} />{/if}
{#if pendingReset}<ConfirmModal open title={`Reset ${pendingReset}?`} message="This disconnects database clients, discards local data, and applies the latest schema. This cannot be undone." confirmLabel="Reset database" danger onConfirm={confirmReset} onCancel={cancelReset} />{/if}
{#if logOpen && operation}<LogModal service={`${operation.op} ${dbOpLabel(operation)}`} lines={operation.lines} onClose={() => logOpen = false} />{/if}

<style>
  .operations { min-width: 0; }
  header { display: flex; justify-content: space-between; align-items: flex-start; gap: var(--space-4); margin-bottom: var(--space-3); }
  h2 { margin: 0; font-size: var(--text-xl); } header p { margin: 3px 0 0; color: var(--dim); font-size: var(--text-sm); } header strong { color: var(--dim); font-size: var(--text-sm); }
  .rows { display: flex; flex-direction: column; gap: var(--space-3); }
  .empty { padding: var(--space-6); border: 1px dashed var(--border); border-radius: var(--radius-lg); text-align: center; color: var(--text-secondary); } .empty p { margin: var(--space-2) 0 0; color: var(--dim); }
  .drawer .rows { gap: 0; }
  .all-failed { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); margin-bottom: var(--space-3); padding: var(--space-3) var(--space-4); border: 1px solid color-mix(in srgb, var(--red) 45%, var(--border)); border-radius: var(--radius-lg); background: color-mix(in srgb, var(--red) 8%, var(--card)); }
  .all-failed span { color: var(--text-secondary); font-size: var(--text-sm); } .all-failed button { margin-left: auto; display: inline-flex; align-items: center; gap: var(--space-1); }
</style>
