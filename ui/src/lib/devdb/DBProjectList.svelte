<script lang="ts">
  import type { DevDBProject } from '$lib/types.gen'
  import type { DriftSummary } from './drift.svelte'
  import { summarizeProject } from './drift.svelte'
  import { FolderGit2, Check, TriangleAlert } from '@lucide/svelte'

  type Props = {
    projects: DevDBProject[]
    workspaceRoot?: string
    driftByDB?: Record<string, DriftSummary>
    selectedPath?: string
    onSelect: (project: DevDBProject) => void
  }

  let { projects, workspaceRoot, driftByDB = {}, selectedPath, onSelect }: Props = $props()
  const projectSummary = $derived(`${projects.length} SQL project${projects.length === 1 ? '' : 's'}`)
</script>

<div class="project-card">
  <div class="card-header">
    <div class="card-title">DB projects</div>
    <div class="card-subtitle">{projectSummary}</div>
  </div>
  <div class="project-list">
    {#each projects as project (project.path)}
      {@const selected = selectedPath === project.path}
      {@const pd = summarizeProject(project.databases, driftByDB)}
      <button type="button" class="project-row" class:selected aria-pressed={selected} aria-label={`Select ${project.name}`} onclick={() => onSelect(project)}>
        <span class="project-icon" aria-hidden="true"><FolderGit2 size={14} /></span>
        <span class="project-info">
          <span class="project-topline">
            <span class="project-name">{project.name}</span>
            {#if pd}
              {#if pd.kind === 'in-sync'}
                <span class="drift-badge ok"><Check size={11} aria-hidden="true" /> In sync</span>
              {:else if pd.kind === 'error'}
                <span class="drift-badge err"><TriangleAlert size={11} aria-hidden="true" /> Check failed</span>
              {:else if pd.kind === 'unchecked'}
                <span class="drift-badge muted">Check required</span>
              {:else}
                <span class="drift-badge" class:warn={!pd.dataLoss} class:err={pd.dataLoss} class:stale={pd.stale} title={pd.stale ? 'Schema status is refreshing' : undefined}>{#if pd.dataLoss}<TriangleAlert size={11} aria-hidden="true" /> {/if}{pd.changes} change{pd.changes === 1 ? '' : 's'}{pd.stale ? ' · refreshing' : pd.dataLoss ? ' · data loss' : ''}</span>
              {/if}
            {/if}
            <span class="project-count">{project.databases.length} DB</span>
          </span>
          <span class="db-tags">
            {#each project.databases.slice(0, 3) as database (database)}
              <span class="db-tag">{database}</span>
            {:else}
              <span class="db-tag">No databases</span>
            {/each}
            {#if project.databases.length > 3}<span class="db-tag">+{project.databases.length - 3}</span>{/if}
          </span>
        </span>
        {#if project.databases.length > 0}<span class="row-chevron" aria-hidden="true">›</span>{/if}
      </button>
    {/each}
  </div>
  {#if workspaceRoot}<div class="root" title={workspaceRoot}>{workspaceRoot}</div>{/if}
</div>

<style>
  .project-card { height: 100%; display: flex; flex-direction: column; background: var(--card); }
  .card-header { padding: var(--space-3) var(--space-4); border-bottom: 1px solid var(--border); }
  .card-title { font-size: var(--text-base); font-weight: 700; text-transform: uppercase; letter-spacing: .04em; }
  .card-subtitle, .root { color: var(--dim); font-size: var(--text-sm); margin-top: 2px; }
  .project-list { flex: 1; overflow-y: auto; }
  .project-row { width: 100%; display: grid; grid-template-columns: 16px minmax(0, 1fr) auto; align-items: center; gap: var(--space-2); padding: .65rem var(--space-4); border: 0; border-top: 1px solid color-mix(in srgb, var(--border) 60%, transparent); background: transparent; color: inherit; cursor: pointer; text-align: left; border-radius: 0; }
  .project-row:first-child { border-top: 0; }
  .project-row:hover { background: color-mix(in srgb, var(--fg) 3%, transparent); }
  .project-row:focus-visible { outline: 2px solid var(--blue); outline-offset: -2px; }
  .project-row.selected { background: color-mix(in srgb, var(--blue) 8%, transparent); box-shadow: inset 3px 0 0 var(--blue); }
  .project-icon, .row-chevron { color: var(--dim); display: inline-flex; }
  .project-row.selected .project-icon, .project-row.selected .row-chevron { color: var(--blue); }
  .project-info, .project-name { min-width: 0; }
  .project-topline, .db-tags { display: flex; align-items: center; gap: var(--space-2); }
  .project-name { font-weight: 600; font-size: var(--text-md); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .project-count, .db-tag { border-radius: var(--radius-pill); font-size: var(--text-xs); padding: 1px 6px; }
  .project-count { margin-left: auto; color: var(--blue); background: color-mix(in srgb, var(--blue) 12%, transparent); font-weight: 700; }
  .drift-badge { display: inline-flex; align-items: center; gap: 3px; margin-left: auto; border-radius: var(--radius-pill); font-size: var(--text-xs); padding: 1px 7px; font-weight: 600; }
  .drift-badge + .project-count { margin-left: var(--space-1); }
  .drift-badge.ok { color: var(--green); background: color-mix(in srgb, var(--green) 15%, transparent); }
  .drift-badge.warn { color: var(--yellow); background: color-mix(in srgb, var(--yellow) 15%, transparent); }
  .drift-badge.err { color: var(--red); background: color-mix(in srgb, var(--red) 15%, transparent); }
  .drift-badge.muted { color: var(--dim); background: color-mix(in srgb, var(--fg) 7%, transparent); }
  .drift-badge.stale { border: 1px dashed color-mix(in srgb, var(--fg) 30%, transparent); background: transparent; color: var(--dim); opacity: 0.85; }
  .db-tags { flex-wrap: wrap; gap: var(--space-1); margin-top: 4px; }
  .db-tag { display: inline-flex; align-items: center; gap: var(--space-1); color: var(--dim); background: color-mix(in srgb, var(--bg) 80%, transparent); border: 1px solid var(--border); border-radius: 3px; }
  .root { padding: var(--space-3) var(--space-4); border-top: 1px solid var(--border); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
