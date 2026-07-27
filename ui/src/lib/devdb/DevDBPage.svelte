<script lang="ts">
  import { push } from 'svelte-spa-router'
  import { store, toast } from '$lib/stores.svelte'
  import { devStore, dbWorkflowHidden } from './stores.svelte'
  import { createElapsed, publishDisabledReason, dbOpLabel, dbOpRunning, createSubmitGuard } from './dbOpView.svelte'
  import { fetchDBProjects, publishAllDBs, refreshDevMeta, fetchResetState, type ResetStateMap } from './api'
  import DevDBHeader from './DevDBHeader.svelte'
  import SQLServerContextBar from './SQLServerContextBar.svelte'
  import DevDBWorkspace from './DevDBWorkspace.svelte'
  import DiffModal from './DiffModal.svelte'
  import LogModal from '$components/LogModal.svelte'
  import { createDriftController } from './drift.svelte'

  let projectsState = $state<'loading' | 'error' | 'ready'>('loading')
  let logOpen = $state(false)
  const elapsed = createElapsed(() => devStore.dbOpInFlight)
  const serviceName = $derived(devStore.devMeta?.sql_server_service || '')
  const service = $derived(serviceName ? store.daemon.services[serviceName] : undefined)
  const running = $derived(dbOpRunning(devStore.dbOpInFlight))
  const runningAll = $derived(running && !!devStore.dbOpInFlight?.all)
  const total = $derived(devStore.dbProjects.reduce((sum, project) => sum + project.databases.length, 0))
  const submitAll = createSubmitGuard(() => dbOpRunning(devStore.dbOpInFlight))
  const disabledReason = $derived(submitAll.reason || publishDisabledReason())

  // Drift (schema-diff) state shared across every project's rows, so one
  // Check-all sweep can populate them all. Opening the Diff modal also
  // refreshes that row's badge through the same controller.
  const drift = createDriftController()
  let diffTarget = $state<string | null>(null)
  function openDiff(database: string) {
    diffTarget = database
  }

  // Live reset readiness per DB (exists + standard/recreate) — drives the
  // pre-click legacy notice and reset-disabled state. Kept on failure
  // (stale beats empty; the 409 gate still protects the run).
  let resetStates = $state<ResetStateMap>({})
  async function loadResetStates() {
    const states = await fetchResetState()
    if (states) resetStates = states
  }

  async function loadProjects() {
    projectsState = 'loading'
    const projects = await fetchDBProjects()
    if (projects === null) { projectsState = 'error'; return }
    devStore.dbProjects = projects
    projectsState = 'ready'
    void loadResetStates()
    await drift.load()
    if (service?.state === 'healthy') void drift.checkOnEntry(projects.flatMap((project) => project.databases))
  }
  $effect(() => { refreshDevMeta(); void loadProjects() })

  // Re-probe reset readiness only on real transitions — an op finishing
  // (database existence may have changed) or the configured target coming up — never on
  // every status frame, since a live probe hits the server per database.
  let prevRunning = false
  let prevHealthy = false
  $effect(() => {
    const isRunning = running
    const healthy = service?.state === 'healthy'
    if (prevRunning && !isRunning) {
      void loadResetStates()
      const op = devStore.dbOpInFlight
      if (op?.ok) {
        if (op.all) void drift.checkFast(devStore.dbProjects.flatMap((project) => project.databases))
        else if (op.db) void drift.refreshDrift(op.db, 'fast')
      }
    }
    if (!prevHealthy && healthy && projectsState === 'ready') {
      void loadResetStates()
      void drift.checkOnEntry(devStore.dbProjects.flatMap((project) => project.databases))
    }
    prevRunning = isRunning
    prevHealthy = healthy
  })

  async function publishAll() {
    if (!submitAll.begin()) return
    toast(`Publishing ${total} databases…`)
    const result = await publishAllDBs()
    if (!result.ok) { submitAll.reset(); toast(result.data?.error ?? 'Failed to start publish') }
  }
  function openService() {
    if (!serviceName) return
    store.graph.selectedNode = serviceName
    void push('/')
  }
  function refreshVisibleDiffs() {
    if (projectsState !== 'ready' || service?.state !== 'healthy') return
    void drift.checkFast(devStore.dbProjects.flatMap((project) => project.databases))
  }
  function refreshAllDiffs() {
    if (service?.state !== 'healthy' || running) return
    void drift.checkAll(devStore.dbProjects.flatMap((project) => project.databases))
  }
  $effect(() => {
    if (service?.state !== 'healthy') diffTarget = null
  })
</script>

<svelte:window onfocus={refreshVisibleDiffs} />

<div class="page">
  {#if dbWorkflowHidden()}<div class="page-notice" role="status"><strong>DB workflow is not available.</strong><p>The active environment has no SQL Server target.</p></div>
  {:else if devStore.devMeta}
    <DevDBHeader running={runningAll} {total} elapsedSeconds={elapsed.seconds} {disabledReason} hasLog={!!devStore.dbOpInFlight} checkingAll={drift.checkingAll} checkProgress={drift.checkProgress} onPublishAll={publishAll} onCheckAll={refreshAllDiffs} onViewLog={() => logOpen = true} />
    <SQLServerContextBar health={service?.state || 'stopped'} targetName={serviceName} environmentName={devStore.devMeta.environment_name} onOpenService={openService} />
    {#if projectsState === 'loading'}<div class="page-notice" role="status" aria-busy="true">Loading projects…</div>
    {:else if projectsState === 'error'}<div class="page-notice" role="alert"><strong>Couldn't load DB projects.</strong><p>The daemon may be busy or unreachable.</p><button type="button" onclick={loadProjects}>Retry</button></div>
    {:else}<DevDBWorkspace projects={devStore.dbProjects} states={devStore.dbState} {resetStates} operation={devStore.dbOpInFlight} workspaceRoot={devStore.devMeta.workspace_root} sqlServerHealthy={service?.state === 'healthy'} elapsedSeconds={elapsed.seconds} {disabledReason} driftByDB={drift.byDB} diffingDBs={drift.diffing} onDiff={openDiff} />{/if}
  {:else}<div class="page-notice" role="status" aria-busy="true">Loading…</div>{/if}
</div>
{#if logOpen && devStore.dbOpInFlight}<LogModal service={`${devStore.dbOpInFlight.op} ${dbOpLabel(devStore.dbOpInFlight)}`} lines={devStore.dbOpInFlight.lines} onClose={() => logOpen = false} />{/if}
{#if diffTarget}<DiffModal database={diffTarget} onResult={(result) => drift.recordResult(result.db, result)} onClose={() => diffTarget = null} />{/if}
<style>
  .page { display: flex; flex-direction: column; gap: var(--space-4); padding: var(--space-4); } .page-notice { margin: var(--space-4) auto; max-width: 480px; padding: var(--space-4); border: 1px solid var(--border); border-radius: var(--radius-lg); color: var(--text-secondary); text-align: center; } .page-notice p { margin-bottom: 0; } .page-notice button { margin-top: var(--space-3); }
</style>
