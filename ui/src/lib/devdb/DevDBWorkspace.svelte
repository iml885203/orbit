<script lang="ts">
  import type { DevDBProject, DBState } from '$lib/types.gen'
  import type { DBOpInFlight } from './stores.svelte'
  import type { ResetStateMap } from './api'
  import type { DriftSummary } from './drift.svelte'
  import DBProjectList from './DBProjectList.svelte'
  import DatabaseOperationsList from './DatabaseOperationsList.svelte'
  import DBTankScene from './DBTankScene.svelte'
  import { deriveSceneState, publishProgressPercent } from './scene-state'
  type Props = { projects: DevDBProject[]; states: Record<string, DBState>; resetStates?: ResetStateMap; operation: DBOpInFlight | null; workspaceRoot?: string; sqlServerHealthy: boolean; elapsedSeconds: number; disabledReason?: string; driftByDB?: Record<string, DriftSummary>; diffingDBs?: Set<string>; onDiff: (database: string) => void }
  let { projects, states, resetStates = {}, operation, workspaceRoot, sqlServerHealthy, elapsedSeconds, disabledReason = '', driftByDB = {}, diffingDBs = new Set(), onDiff }: Props = $props()
  let selectedProjectPath = $state<string | undefined>()
  const selectedProject = $derived(projects.find((project) => project.path === selectedProjectPath))
  // Only animate the tank for an op that targets THIS project — an --all
  // run or one publishing a database this project owns (mirrors
  // DatabaseRow's per-row scoping).
  const scopedOp = $derived(
    operation && (operation.all || selectedProject?.databases.includes(operation.db)) ? operation : null,
  )
  const sceneState = $derived(deriveSceneState(scopedOp, sqlServerHealthy, !!selectedProject))
  const progressPercent = $derived(publishProgressPercent(scopedOp, elapsedSeconds))
  $effect(() => { if (!projects.length) { selectedProjectPath = undefined; return } if (!selectedProjectPath || !projects.some((project) => project.path === selectedProjectPath)) selectedProjectPath = projects.find((project) => project.databases.length)?.path ?? projects[0].path })
</script>
{#if projects.length === 0}
  <div class="empty"><strong>No DB projects found.</strong><p>Check the DB project configuration or run <code>orbit env sync</code>.</p></div>
{:else}
  <div class="workspace"><nav aria-label="Database projects"><DBProjectList {projects} {workspaceRoot} {driftByDB} selectedPath={selectedProjectPath} onSelect={(project) => selectedProjectPath = project.path} /></nav><main>{#if selectedProject}<div class="scene"><DBTankScene state={sceneState} projectName={selectedProject.name} {progressPercent} /></div><DatabaseOperationsList project={selectedProject} {states} {resetStates} {operation} {disabledReason} {driftByDB} {diffingDBs} {onDiff} />{/if}</main></div>
{/if}
<style>
  .workspace { display: grid; grid-template-columns: minmax(280px, 320px) minmax(0, 1fr); min-height: 430px; border: 1px solid var(--border); border-radius: var(--radius-lg); overflow: hidden; background: var(--card); } nav { border-right: 1px solid var(--border); min-width: 0; } main { display: grid; grid-template-columns: minmax(0, 1.15fr) minmax(0, 1fr); align-items: start; gap: var(--space-4); padding: var(--space-4); min-width: 0; } .scene { min-width: 0; position: sticky; top: var(--space-4); } .empty { padding: var(--space-6); text-align: center; border: 1px dashed var(--border); border-radius: var(--radius-lg); color: var(--text-secondary); } .empty p { color: var(--dim); }
  @media (max-width: 1120px) { main { grid-template-columns: 1fr; } .scene { position: static; } }
  @media (max-width: 760px) { .workspace { grid-template-columns: 1fr; } nav { border-right: 0; border-bottom: 1px solid var(--border); max-height: 260px; } }
</style>
