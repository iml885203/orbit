<script lang="ts">
  import { push } from 'svelte-spa-router'
  import { Eye } from '@lucide/svelte'
  import { store, toast } from '$lib/stores.svelte'
  import { fetchEnvs, fetchGraph, mutateSource } from '$lib/api'

  let adding = $state(false)
  let busySource = $state('')
  let sourceName = $state('')
  let sourceType = $state<'git' | 'local'>('git')
  let sourceLocation = $state('')
  let sourceWorkspace = $state('')

  function close() {
    store.ui.envPopoverOpen = false
  }

  async function preview(identity: string) {
    if (identity === store.graph.data?.env) {
      store.graph.preview = null
      close()
      await push('/')
      return
    }
    const graph = await fetchGraph(identity)
    if (!graph) {
      toast(`Failed to load ${identity}`)
      return
    }
    store.graph.preview = graph
    close()
    await push('/')
  }

  async function runSourceAction(name: string, action: Record<string, unknown>) {
    busySource = name
    try {
      const result = await mutateSource(action)
      if (!result.ok) {
        toast(result.data?.error || `Failed to update ${name}`)
        return false
      }
      store.daemon.envs = await fetchEnvs()
      toast(result.data?.message || `Updated ${name}`)
      return true
    } finally {
      busySource = ''
    }
  }

  async function addSource() {
    if (!sourceName || !sourceLocation) return
    const payload: Record<string, unknown> = {
      action: 'add', name: sourceName, type: sourceType, workspace: sourceWorkspace,
      [sourceType === 'git' ? 'url' : 'path']: sourceLocation,
    }
    if (await runSourceAction(sourceName, payload)) {
      adding = false
      sourceName = ''
      sourceLocation = ''
      sourceWorkspace = ''
    }
  }

  async function editSource(name: string, type: string, current: string) {
    const location = window.prompt(`New ${type === 'git' ? 'Git URL' : 'local path'} for ${name}`, current)
    if (!location || location === current) return
    await runSourceAction(name, { action: 'update', name, type, [type === 'git' ? 'url' : 'path']: location })
  }

  async function editRef(name: string, current: string) {
    const ref = window.prompt(`Git ref for ${name} (leave empty to follow the default branch)`, current)
    if (ref === null || ref === current) return
    await runSourceAction(name, ref ? { action: 'update', name, ref } : { action: 'update', name, clear_ref: true })
  }

  async function editWorkspace(name: string, current: string) {
    const workspace = window.prompt(`Workspace for ${name} (leave empty to clear)`, current)
    if (workspace === null) return
    await runSourceAction(name, workspace ? { action: 'set_workspace', name, workspace } : { action: 'clear_workspace', name })
  }

  async function removeSource(name: string) {
    if (!window.confirm(`Remove environment source ${name}? A selected stopped environment will be cleared.`)) return
    await runSourceAction(name, { action: 'remove', name, confirmed: true })
  }
</script>

{#if store.ui.envPopoverOpen}
  <div
    class="backdrop"
    role="button"
    tabindex="-1"
    onclick={close}
    onkeydown={(e) => e.key === 'Escape' && close()}
  ></div>
  <div class="popover" role="dialog" aria-label="Environment selector">
    <div class="header">
      <span class="title">Environments</span>
      <button class="close" onclick={close} aria-label="Close">×</button>
    </div>
    <div class="hint">Select an environment to preview it safely.</div>
    <div class="source-toolbar">
      <button class="source-add-toggle" type="button" onclick={() => adding = !adding}>{adding ? 'Cancel adding source' : 'Add source'}</button>
      <button class="source-add-toggle" type="button" disabled={busySource !== ''} onclick={() => runSourceAction('all sources', { action: 'sync_all' })}>Sync all</button>
    </div>
    {#if adding}
      <form class="source-form" onsubmit={(event) => { event.preventDefault(); addSource() }}>
        <label>Source name<input bind:value={sourceName} required /></label>
        <label>Type<select bind:value={sourceType}><option value="git">Git</option><option value="local">Local</option></select></label>
        <label>{sourceType === 'git' ? 'Git URL' : 'Local path'}<input bind:value={sourceLocation} required /></label>
        <label>Workspace<input bind:value={sourceWorkspace} placeholder="Optional local checkout" /></label>
        <button type="submit" disabled={busySource !== ''} aria-busy={busySource !== ''}>Add and sync</button>
      </form>
    {/if}
    {#if store.daemon.envs?.context}
      {@const context = store.daemon.envs.context}
      <section class="context" aria-label="Active environment context">
        <div class="context-title">
          <strong>{context.display_name}</strong>
          <span class="context-kind">{context.kind === 'project' ? 'Project environment' : context.kind === 'explicit' ? 'Explicit config' : 'Managed environment'}</span>
          {#if !context.available}<span class="unavailable" role="status">Unavailable</span>{/if}
        </div>
        <div class="path" title={context.config_path}>{context.config_path}</div>
        {#if context.project_root}
          <div class="metadata"><span>Project root</span><code>{context.project_root}</code></div>
        {/if}
        {#if context.managed_selection && !context.managed_selection.active}
          <div class="metadata"><span>Managed selection</span><code>{context.managed_selection.name}</code><em>not active</em></div>
        {/if}
        {#if !context.available}
          {#if context.managed_selection}
            <button class="recovery" onclick={() => preview(context.managed_selection!.identity || context.managed_selection!.name)}>Preview managed environment {context.managed_selection.identity || context.managed_selection.name}</button>
          {:else}
            <div class="recovery-hint">Run <code>orbit init</code> to choose an available environment.</div>
          {/if}
        {/if}
      </section>
    {/if}
    <ul aria-label="Managed environment sources">
      {#each store.daemon.envs?.sources ?? [] as source (source.name)}
        <li class="source-group">
          <div class="source-header">
            <strong>{source.name}</strong>
            <span>{source.type}</span>
            {#if source.default}<span class="source-default">default</span>{/if}
          </div>
          <div class="source-location" title={source.location}>{source.location}</div>
          {#if source.workspace}<div class="source-location" title={source.workspace}>Workspace: {source.workspace}</div>{/if}
          {#if source.last_sync_error}<div class="source-error" role="status">{source.last_sync_error}</div>{/if}
          <div class="source-actions">
            <button type="button" disabled={busySource !== ''} aria-busy={busySource === source.name} onclick={() => runSourceAction(source.name, { action: 'sync', name: source.name })}>Sync</button>
            {#if !source.default}<button type="button" disabled={busySource !== ''} onclick={() => runSourceAction(source.name, { action: 'set_default', name: source.name })}>Set default</button>{/if}
            <button type="button" disabled={busySource !== ''} onclick={() => editSource(source.name, source.type, source.location)}>Edit location</button>
            {#if source.type === 'git'}<button type="button" disabled={busySource !== ''} onclick={() => editRef(source.name, source.ref || '')}>Edit ref</button>{/if}
            <button type="button" disabled={busySource !== ''} onclick={() => editWorkspace(source.name, source.workspace || '')}>Workspace</button>
            {#if source.type === 'git' && source.ref}<button type="button" disabled={busySource !== ''} onclick={() => runSourceAction(source.name, { action: 'update', name: source.name, clear_ref: true })}>Follow default branch</button>{/if}
            <button type="button" class="danger" disabled={busySource !== ''} onclick={() => removeSource(source.name)}>Remove</button>
          </div>
          {#each source.environments as env (env.identity)}
            {@const previewing = store.graph.preview?.env === env.identity}
            <button
              class="env-row"
              class:current={env.selected}
              class:previewing
              onclick={() => preview(env.identity)}
              title={env.path}
            >
              <span class="dot" class:active={env.running}></span>
              <span class="name">{env.name}</span>
              {#if env.selected}<span class="badge">selected</span>{/if}
              {#if env.running}<span class="badge">running</span>{/if}
              {#if previewing}<span class="badge preview"><Eye size={11} aria-hidden="true" /> preview</span>{/if}
            </button>
          {/each}
        </li>
      {/each}
    </ul>
    <div class="footer">
      Previewing never stops services. Activate the environment from the Services page.
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.3);
    z-index: 99;
  }
  .popover {
    position: fixed;
    top: 3.5rem;
    right: var(--space-5);
    width: 320px;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: 0 6px 24px rgba(0, 0, 0, 0.4);
    padding: var(--space-3);
    z-index: 100;
  }
  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.35rem; /* off-grid: tight internal spacing */
  }
  .title {
    font-weight: 600;
    font-size: var(--text-base);
  }
  .close {
    background: none;
    border: none;
    color: var(--dim);
    font-size: 1.1rem; /* above scale — keep hardcoded */
    cursor: pointer;
    padding: 0 0.3rem; /* off-grid: intentional minimal close hit area */
  }
  .close:hover { color: var(--fg); }
  .hint {
    font-size: var(--text-sm);
    color: var(--dim);
    margin-bottom: var(--space-2);
  }
  .context {
    margin-bottom: var(--space-2);
    padding: var(--space-2);
    border: 1px solid color-mix(in srgb, var(--blue) 35%, var(--border));
    border-radius: var(--radius-md);
    background: color-mix(in srgb, var(--blue) 6%, transparent);
  }
  .context-title {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .context-kind, .unavailable {
    color: var(--blue);
    font-size: var(--text-xs);
  }
  .unavailable { color: var(--red); }
  .path {
    margin-top: var(--space-1);
    color: var(--dim);
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .metadata {
    display: flex;
    gap: var(--space-2);
    margin-top: var(--space-1);
    color: var(--dim);
    font-size: var(--text-xs);
  }
  .metadata code { color: var(--fg); }
  .metadata em { margin-left: auto; color: var(--yellow); font-style: normal; }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 280px;
    overflow-y: auto;
  }
  .source-group { padding: var(--space-2) 0; border-bottom: 1px solid var(--border); }
  .source-group:last-child { border-bottom: 0; }
  .source-header { display: flex; align-items: center; gap: var(--space-2); font-size: var(--text-sm); }
  .source-header span, .source-location { color: var(--dim); font-size: var(--text-xs); }
  .source-default { color: var(--blue) !important; }
  .source-location { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .source-error { color: var(--red); font-size: var(--text-xs); }
  .source-add-toggle, .source-actions button, .source-form button {
    border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--card); color: var(--fg);
    cursor: pointer; font-size: var(--text-xs); padding: var(--space-1) var(--space-2);
  }
  .source-form { display: grid; gap: var(--space-2); margin: var(--space-2) 0; padding: var(--space-2); border: 1px solid var(--border); border-radius: var(--radius-md); }
  .source-form label { display: grid; gap: var(--space-1); color: var(--dim); font-size: var(--text-xs); }
  .source-form input, .source-form select { background: var(--bg); border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--fg); padding: var(--space-1); }
  .source-actions { display: flex; flex-wrap: wrap; gap: var(--space-1); margin: var(--space-1) 0; }
  .source-toolbar { display: flex; gap: var(--space-1); }
  .source-actions .danger { color: var(--red); }
  .env-row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    color: var(--fg);
    padding: 0.4rem var(--space-2); /* 0.4rem off-grid vertical */
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-size: var(--text-md);
    font-family: inherit;
  }
  .env-row:hover { background: rgba(255, 255, 255, 0.05); }
  .env-row.current { color: var(--fg); }
  .env-row.previewing { background: color-mix(in srgb, var(--blue) 9%, transparent); }
  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--border);
    flex-shrink: 0;
  }
  .dot.active { background: var(--green); }
  .name { flex: 1; }
  .badge {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    font-size: var(--text-xs);
    color: var(--green);
    background: rgba(46, 160, 67, 0.15);
    padding: 0.05rem 0.4rem; /* off-grid: intentional tiny badge padding */
    border-radius: 3px; /* off-grid: between 0 and radius-sm */
  }
  .badge.preview {
    color: var(--blue);
    background: color-mix(in srgb, var(--blue) 15%, transparent);
  }
  .footer {
    margin-top: var(--space-2);
    padding-top: var(--space-2);
    border-top: 1px solid var(--border);
    font-size: var(--text-sm);
    color: var(--dim);
  }
</style>
