<script lang="ts">
  import { push } from 'svelte-spa-router'
  import { Eye } from '@lucide/svelte'
  import { store, toast } from '$lib/stores.svelte'
  import { fetchGraph, mutateSource } from '$lib/api'
  import SourceManager from './SourceManager.svelte'

  let managingSources = $state(false)

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

  async function acknowledgeMigration(review: boolean) {
	try {
	  const result = await mutateSource({ action: 'ack_migration' })
	  if (!result.ok) {
	    toast(result.data?.error || 'Could not dismiss the migration summary')
	    return
	  }
	  store.ui.sourceMigrationNoticeSeen = true
	  if (review) managingSources = true
	} catch {
	  toast('Could not dismiss the migration summary')
	}
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
  <div class="popover" class:wide={managingSources} role="dialog" aria-label={managingSources ? 'Environment source manager' : 'Environment selector'}>
    {#if managingSources}
      <SourceManager onback={() => managingSources = false} />
    {:else}
    <div class="header">
      <span class="title">Environments</span>
      <button class="close" onclick={close} aria-label="Close">×</button>
    </div>
    <div class="hint">Select an environment to preview it safely.</div>
    {#if store.daemon.envs?.migration && !store.ui.sourceMigrationNoticeSeen}
      {@const migration = store.daemon.envs.migration}
      <section class="migration-notice" role="status" aria-label="Environment source migration complete">
        <div><strong>Source migration complete</strong><button type="button" aria-label="Dismiss migration summary" onclick={() => acknowledgeMigration(false)}>×</button></div>
        <p>Moved your previous environment setup to <code>{migration.source_name}</code> from <code>{migration.location}</code>{migration.ref ? ` at ${migration.ref}` : ''} offline. {migration.cached_environments} cached {migration.cached_environments === 1 ? 'environment was' : 'environments were'} preserved{migration.selection_preserved ? ', including your selection' : ''}{migration.workspace_preserved ? ' and workspace' : ''}.</p>
        <button type="button" onclick={() => acknowledgeMigration(true)}>Review and sync source</button>
      </section>
    {/if}
    <button class="manage-sources" type="button" onclick={() => managingSources = true}>Manage sources <span>{store.daemon.envs?.sources.length ?? 0}</span></button>
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
    {/if}
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
    max-height: calc(100vh - 4.5rem);
    overflow-y: auto;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: 0 6px 24px rgba(0, 0, 0, 0.4);
    padding: var(--space-3);
    z-index: 100;
  }
  .popover.wide { width: min(620px, calc(100vw - 2 * var(--space-5))); }
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
  .source-header span { color: var(--dim); font-size: var(--text-xs); }
  .source-default { color: var(--blue) !important; }
  .manage-sources {
    display: flex; align-items: center; justify-content: space-between; width: 100%; margin-bottom: var(--space-2);
    border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--card); color: var(--fg);
    cursor: pointer; font-size: var(--text-xs); padding: var(--space-1) var(--space-2);
  }
  .manage-sources span { color: var(--dim); }
  .migration-notice { display: grid; gap: var(--space-2); margin-bottom: var(--space-2); padding: var(--space-2); border: 1px solid color-mix(in srgb, var(--green) 40%, var(--border)); border-radius: var(--radius-md); background: color-mix(in srgb, var(--green) 6%, transparent); }
  .migration-notice > div { display: flex; align-items: center; justify-content: space-between; }
  .migration-notice p { margin: 0; color: var(--dim); font-size: var(--text-xs); }
  .migration-notice button { border: 0; background: none; color: var(--blue); cursor: pointer; padding: 0; text-align: left; }
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
