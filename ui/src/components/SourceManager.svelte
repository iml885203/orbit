<script lang="ts">
  import { ArrowLeft, ChevronDown } from '@lucide/svelte'
  import { fetchEnvs, mutateSource } from '$lib/api'
  import { store, toast } from '$lib/stores.svelte'
  import type { EnvironmentSourceInfo } from '$lib/types.gen'

  let { onback }: { onback: () => void } = $props()
  let adding = $state(false)
  let busySource = $state('')
  let actionError = $state('')
  let sourceName = $state('')
  let sourceType = $state<'git' | 'local'>('git')
  let sourceLocation = $state('')
  let sourceRef = $state('')
  let sourceWorkspace = $state('')
  let editingSource = $state('')
  let editLocation = $state('')
  let editRef = $state('')
  let editWorkspace = $state('')
  let removingSource = $state('')

  const sources = $derived(store.daemon.envs?.sources ?? [])

  async function runSourceAction(name: string, action: Record<string, unknown>, success: string) {
    busySource = name
    actionError = ''
    try {
      const result = await mutateSource(action)
      const mutationError = result.ok ? '' : (result.data?.error || `Failed to update ${name}`)
	  let refreshed
	  try {
	    refreshed = await fetchEnvs()
	  } catch {
	    if (mutationError) {
	      actionError = `${mutationError} The latest source state also could not be loaded.`
	      toast(actionError)
	      return false
	    }
	    throw new Error('refresh failed')
	  }
      if (refreshed) store.daemon.envs = refreshed
      if (mutationError) {
        actionError = mutationError
        toast(actionError)
        return false
      }
      if (!refreshed) {
        actionError = `${success}, but the latest source state could not be loaded. Refresh the dashboard.`
        toast(actionError)
        return false
      }
      toast(success)
      return true
    } catch {
      actionError = `Could not update ${name}. The operation may have completed; refresh the dashboard before retrying.`
      toast(actionError)
      return false
    } finally {
      busySource = ''
    }
  }

  async function addSource() {
    const payload: Record<string, unknown> = {
      action: 'add',
      name: sourceName.trim(),
      type: sourceType,
      workspace: sourceWorkspace.trim(),
      [sourceType === 'git' ? 'url' : 'path']: sourceLocation.trim(),
    }
    if (sourceType === 'git' && sourceRef.trim()) payload.ref = sourceRef.trim()
    if (await runSourceAction(sourceName, payload, `Added and synced ${sourceName}`)) {
      adding = false
      sourceName = ''
      sourceLocation = ''
      sourceRef = ''
      sourceWorkspace = ''
      onback()
    }
  }

  function beginEdit(source: EnvironmentSourceInfo) {
    editingSource = editingSource === source.name ? '' : source.name
    editLocation = source.location
    editRef = source.ref ?? ''
    editWorkspace = source.workspace ?? ''
  }

  async function saveSettings(source: EnvironmentSourceInfo) {
	if (!editLocation.trim()) {
	  actionError = `${source.type === 'git' ? 'Git URL' : 'Local directory'} is required.`
	  return
	}
    const locationChanged = editLocation.trim() !== source.location
    const refChanged = source.type === 'git' && editRef.trim() !== (source.ref ?? '')
    const workspaceChanged = editWorkspace.trim() !== (source.workspace ?? '')

    if (locationChanged || refChanged || workspaceChanged) {
      const update: Record<string, unknown> = { action: 'update', name: source.name, type: source.type }
      if (locationChanged) update[source.type === 'git' ? 'url' : 'path'] = editLocation.trim()
      if (refChanged) {
        if (editRef.trim()) update.ref = editRef.trim()
        else update.clear_ref = true
      }
	  if (workspaceChanged) {
	    if (editWorkspace.trim()) update.workspace = editWorkspace.trim()
	    else update.clear_workspace = true
	  }
	  const success = locationChanged || refChanged ? `Updated and synced ${source.name}` : `Updated ${source.name}`
	  if (!await runSourceAction(source.name, update, success)) return
    }
    editingSource = ''
  }

  function syncFreshness(source: EnvironmentSourceInfo) {
    if (source.last_sync_error) return 'Sync failed'
    if (!source.last_sync_at) return 'Never synced'
    const synced = new Date(source.last_sync_at)
    if (Number.isNaN(synced.valueOf())) return 'Synced'
	const minutes = Math.round((synced.valueOf() - Date.now()) / 60_000)
	const formatter = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })
	if (Math.abs(minutes) < 60) return `Synced ${formatter.format(minutes, 'minute')}`
	const hours = Math.round(minutes / 60)
	if (Math.abs(hours) < 24) return `Synced ${formatter.format(hours, 'hour')}`
	const days = Math.round(hours / 24)
	if (Math.abs(days) < 30) return `Synced ${formatter.format(days, 'day')}`
	return `Synced ${synced.toLocaleDateString('en', { year: 'numeric', month: 'short', day: 'numeric' })}`
  }

  function removalBlocker(source: EnvironmentSourceInfo) {
    const running = source.environments.find((environment) => environment.running)
    if (running) return `Stop or switch away from ${running.identity} before removing this source.`
    if (source.default && sources.length > 1) return 'Open another source’s Manage menu and choose Make default before removing this one.'
    return ''
  }

  async function confirmRemove(source: EnvironmentSourceInfo) {
    if (await runSourceAction(source.name, { action: 'remove', name: source.name, confirmed: true }, `Removed ${source.name}`)) {
      removingSource = ''
    }
  }
</script>

<section class="manager" aria-label="Environment source management">
  <div class="manager-header">
    <button class="icon-button" type="button" aria-label="Back to environments" onclick={onback}><ArrowLeft size={16} /></button>
    <div><h2>Sources</h2><p>Add and sync the places Orbit loads environments from.</p></div>
  </div>

  <div class="primary-actions">
    <button class="primary" type="button" onclick={() => { adding = !adding; actionError = '' }}>{adding ? 'Cancel' : 'Add source'}</button>
    {#if sources.length > 1}
      <button type="button" disabled={busySource !== ''} aria-busy={busySource === 'all sources'} onclick={() => runSourceAction('all sources', { action: 'sync_all' }, 'Synced all sources')}>Sync all</button>
    {/if}
  </div>

  {#if actionError}<div class="action-error" role="alert">{actionError}</div>{/if}

  {#if adding}
    <form class="add-form" aria-label="Add environment source" onsubmit={(event) => { event.preventDefault(); addSource() }}>
      <fieldset>
        <legend>Source type</legend>
        <label class:chosen={sourceType === 'git'}><input type="radio" bind:group={sourceType} value="git" />Git repository<span>Shared and updated with Git</span></label>
        <label class:chosen={sourceType === 'local'}><input type="radio" bind:group={sourceType} value="local" />Local directory<span>Use files already on this computer</span></label>
      </fieldset>
      <label>Name<input bind:value={sourceName} required autocomplete="off" placeholder="team" /><small>Used in environment names, such as <code>{sourceName || 'team'}/development</code>.</small></label>
      <label>{sourceType === 'git' ? 'Git URL' : 'Local directory'}<input bind:value={sourceLocation} required placeholder={sourceType === 'git' ? 'https://example.com/team/envs.git' : '/path/to/environments'} /></label>
      {#if sourceType === 'git'}<label>Branch or tag <span>optional</span><input bind:value={sourceRef} placeholder="Follow the default branch" /></label>{/if}
      <label>Workspace <span>optional</span><input bind:value={sourceWorkspace} placeholder="Local checkout used to resolve project paths" /><small>Only needed when environment paths refer to a separate local checkout.</small></label>
      <div class="form-outcome">Orbit validates the source, syncs it, and shows the environments it finds.</div>
      <button class="primary" type="submit" disabled={busySource !== '' || !sourceName.trim() || !sourceLocation.trim()} aria-busy={busySource !== ''}>{busySource ? 'Validating and syncing…' : 'Add and sync'}</button>
    </form>
  {/if}

  {#if sources.length === 0 && !adding}
    <div class="empty"><strong>No sources yet</strong><p>Add a Git repository or local directory to discover environments.</p></div>
  {/if}

  <div class="source-list">
    {#each sources as source (source.name)}
      {@const blocker = removalBlocker(source)}
      {@const selected = source.environments.find((environment) => environment.selected)}
      <article class="source-card">
        <div class="source-summary">
          <div class="source-title"><strong>{source.name}</strong><span>{source.type}</span>{#if source.default}<span class="default-badge">default</span>{/if}</div>
          <div class="location" title={source.location}>{source.location}</div>
          <div class="facts">
            <span class:error={!!source.last_sync_error}>{syncFreshness(source)}</span>
            {#if source.type === 'git' && source.resolved_ref}<code>{source.resolved_ref}{source.commit ? ` · ${source.commit.slice(0, 8)}` : ''}</code>{/if}
            <span>{source.environments.length} {source.environments.length === 1 ? 'environment' : 'environments'}</span>
          </div>
          {#if source.last_sync_error}<div class="sync-error" role="status">{source.last_sync_error}</div>{/if}
        </div>
        <div class="card-actions">
          <button class="primary" type="button" disabled={busySource !== ''} aria-busy={busySource === source.name} onclick={() => runSourceAction(source.name, { action: 'sync', name: source.name }, `Synced ${source.name}`)}>{busySource === source.name ? 'Working…' : 'Sync'}</button>
          <button class="details-toggle" type="button" aria-expanded={editingSource === source.name} onclick={() => beginEdit(source)}>Manage <ChevronDown size={14} /></button>
        </div>

        {#if editingSource === source.name}
          <div class="settings" aria-label="Manage {source.name}">
            <label>{source.type === 'git' ? 'Git URL' : 'Local directory'}<input bind:value={editLocation} /></label>
            {#if source.type === 'git'}<label>Branch or tag<input bind:value={editRef} placeholder="Follow the default branch" /></label>{/if}
            <label>Workspace<input bind:value={editWorkspace} placeholder="No workspace" /></label>
            <div class="settings-actions">
              <button type="button" disabled={busySource !== ''} onclick={() => saveSettings(source)}>Save changes</button>
              {#if !source.default}<button type="button" disabled={busySource !== ''} onclick={() => runSourceAction(source.name, { action: 'set_default', name: source.name }, `${source.name} is now the default source`)}>Make default</button>{/if}
            </div>
            <div class="remove-area">
              {#if blocker}<p class="blocker" role="status">{blocker}</p>{/if}
              <button class="danger" type="button" disabled={busySource !== '' || !!blocker} aria-describedby={blocker ? `remove-blocker-${source.name}` : undefined} onclick={() => removingSource = source.name}>Remove source</button>
              {#if blocker}<span class="sr-only" id="remove-blocker-{source.name}">{blocker}</span>{/if}
            </div>
          </div>
        {/if}

        {#if removingSource === source.name}
          <div class="remove-confirm" role="group" aria-label="Remove source {source.name}">
            <strong>Remove {source.name}?</strong>
            {#if selected}<p>The selected environment <code>{selected.identity}</code> will be cleared.</p>{/if}
            <p>Orbit will remove its managed source data.{source.type === 'local' ? ' Your local directory and its files will remain untouched.' : ''}</p>
            <div><button type="button" onclick={() => removingSource = ''}>Cancel</button><button class="danger" type="button" disabled={busySource !== ''} aria-busy={busySource === source.name} onclick={() => confirmRemove(source)}>Remove source</button></div>
          </div>
        {/if}
      </article>
    {/each}
  </div>
</section>

<style>
  .manager { display: grid; gap: var(--space-3); }
  .manager-header { display: flex; align-items: flex-start; gap: var(--space-2); }
  h2, p { margin: 0; }
  h2 { font-size: var(--text-base); }
  .manager-header p, .empty p { color: var(--dim); font-size: var(--text-sm); }
  button { min-height: 30px; border: 1px solid var(--border); border-radius: var(--radius-md); background: var(--card); color: var(--fg); cursor: pointer; padding: var(--space-1) var(--space-2); }
  button:disabled { cursor: not-allowed; opacity: 0.55; }
  .icon-button { display: grid; place-items: center; width: 30px; padding: 0; }
  .primary { border-color: color-mix(in srgb, var(--blue) 55%, var(--border)); color: var(--blue); }
  .primary-actions, .card-actions, .settings-actions, .remove-confirm > div { display: flex; gap: var(--space-2); }
  .add-form { display: grid; gap: var(--space-3); padding: var(--space-3); border: 1px solid var(--border); border-radius: var(--radius-lg); background: var(--bg); }
  fieldset { display: grid; grid-template-columns: 1fr 1fr; gap: var(--space-2); margin: 0; padding: 0; border: 0; }
  legend { grid-column: 1 / -1; margin-bottom: var(--space-1); color: var(--dim); font-size: var(--text-xs); }
  fieldset label { display: grid; grid-template-columns: auto 1fr; align-items: center; gap: 0 var(--space-2); padding: var(--space-2); border: 1px solid var(--border); border-radius: var(--radius-md); }
  fieldset label.chosen { border-color: var(--blue); }
  fieldset label span { grid-column: 2; }
  label { display: grid; gap: var(--space-1); color: var(--dim); font-size: var(--text-xs); }
  label > span { color: var(--muted); }
  input { min-width: 0; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--card); color: var(--fg); padding: var(--space-2); }
  small, .form-outcome { color: var(--dim); font-size: var(--text-xs); }
  .source-list { display: grid; gap: var(--space-2); max-height: 460px; overflow-y: auto; }
  .source-card { position: relative; display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: var(--space-2); padding: var(--space-3); border: 1px solid var(--border); border-radius: var(--radius-lg); background: var(--card); }
  .source-title, .facts { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); }
  .source-title span, .facts { color: var(--dim); font-size: var(--text-xs); }
  .default-badge { color: var(--blue) !important; }
  .location { overflow: hidden; margin-top: var(--space-1); color: var(--dim); font-family: var(--font-mono); font-size: var(--text-xs); text-overflow: ellipsis; white-space: nowrap; }
  .facts { margin-top: var(--space-2); }
  .facts code { color: var(--fg); }
  .error, .sync-error, .action-error, .blocker { color: var(--red); }
  .sync-error, .action-error, .blocker { font-size: var(--text-xs); }
  .action-error { padding: var(--space-2); border: 1px solid color-mix(in srgb, var(--red) 45%, var(--border)); border-radius: var(--radius-md); }
  .card-actions { align-items: flex-start; }
  .details-toggle { display: inline-flex; align-items: center; gap: var(--space-1); }
  .settings, .remove-confirm { grid-column: 1 / -1; display: grid; gap: var(--space-2); padding-top: var(--space-2); border-top: 1px solid var(--border); }
  .settings { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .settings-actions, .remove-area { grid-column: 1 / -1; }
  .remove-area { display: flex; align-items: center; justify-content: space-between; gap: var(--space-2); padding-top: var(--space-2); border-top: 1px solid var(--border); }
  .danger { color: var(--red); }
  .remove-confirm { padding: var(--space-3); border: 1px solid color-mix(in srgb, var(--red) 45%, var(--border)); border-radius: var(--radius-md); }
  .remove-confirm p { color: var(--dim); font-size: var(--text-sm); }
  .empty { padding: var(--space-4); border: 1px dashed var(--border); border-radius: var(--radius-lg); text-align: center; }
  .sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; }
</style>
