<script lang="ts">
  import type { DiffResult, DiffOp } from '$lib/types.gen'
  import { diffDB } from './api'
  import { Loader2, Check, TriangleAlert, GitCompareArrows, X } from '@lucide/svelte'

  let { database, onClose, onResult }: { database: string; onClose: () => void; onResult?: (result: DiffResult) => void } = $props()

  type Tab = 'summary' | 'script'
  let tab = $state<Tab>('summary')

  let loading = $state(true)
  let error = $state<string | null>(null)
  let result = $state<DiffResult | null>(null)
  let needsEngine = $state(false)
  let exact = $state(false)

  // Script is fetched lazily — only when the SQL tab is first opened — so
  // the common Summary case runs one sqlpackage pass, not two.
  let scriptLoading = $state(false)
  let scriptError = $state<string | null>(null)
  let script = $state<string | null>(null)

  const grouped = $derived.by(() => {
    const g: Record<'Drop' | 'Alter' | 'Create', DiffOp[]> = { Drop: [], Alter: [], Create: [] }
    for (const op of result?.ops ?? []) {
      if (op.action === 'Drop' || op.action === 'Alter' || op.action === 'Create') g[op.action].push(op)
    }
    return g
  })

  function shortType(t: string): string {
    return t.startsWith('Sql') ? t.slice(3) : t
  }

  async function loadSummary(mode: 'fast' | 'analyze' = 'fast') {
    loading = true
    error = null
    exact = mode === 'analyze'
    const { ok, data } = await diffDB(database, false, mode)
    loading = false
    if (ok && data?.needs_engine) {
      needsEngine = true
      result = null
      return
    }
    if (!ok || !data?.result) {
      error = data?.error ?? 'Diff failed — check the daemon logs.'
      return
    }
    needsEngine = false
    result = data.result
    onResult?.(data.result)
  }

  async function loadScript() {
    if (script !== null || scriptLoading) return
    scriptLoading = true
    scriptError = null
    const { ok, data } = await diffDB(database, true)
    scriptLoading = false
    if (!ok || data?.script === undefined) {
      scriptError = data?.error ?? 'Could not generate the deployment script.'
      return
    }
    script = data.script
  }

  function selectTab(t: Tab) {
    tab = t
    if (t === 'script') void loadScript()
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') onClose()
  }

  $effect(() => {
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => { document.body.style.overflow = prev }
  })

  // Kick off the summary diff on mount.
  $effect(() => { void loadSummary() })
</script>

<svelte:window onkeydown={onKey} />

<div class="backdrop" onclick={onClose} role="presentation">
  <div class="modal" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label="Schema changes for {database}" tabindex="-1">
    <header>
      <span class="title">Changes · <strong>{database}</strong></span>
      <div class="tabs" role="tablist" aria-label="Schema change view">
        <button role="tab" aria-selected={tab === 'summary'} class:active={tab === 'summary'} onclick={() => selectTab('summary')}>Overview</button>
        <button role="tab" aria-selected={tab === 'script'} class:active={tab === 'script'} onclick={() => selectTab('script')}>Publish SQL</button>
      </div>
      <button type="button" class="close" onclick={onClose} aria-label="Close"><X size={15} aria-hidden="true" /></button>
    </header>

    <div class="body">
      {#if tab === 'summary'}
        {#if loading}
          <div class="state" role="status"><Loader2 size={22} class="spin" aria-hidden="true" /><p>{exact ? 'Analyzing database impact…' : 'Checking recent schema changes…'}</p><small>{exact ? 'Comparing the project with SQL Server — this can take a few seconds.' : 'Comparing project files with the last publish.'}</small></div>
        {:else if error}
          <div class="state err" role="alert"><TriangleAlert size={22} aria-hidden="true" /><p>{error}</p><button class="retry" onclick={() => void loadSummary(exact ? 'analyze' : 'fast')}>Retry</button></div>
        {:else if needsEngine}
          <div class="state" role="status"><GitCompareArrows size={22} aria-hidden="true" /><p>Database check required</p><small>Check this database once to start tracking schema changes automatically.</small><button class="primary-action" aria-busy={loading} onclick={() => void loadSummary('analyze')}>Check database</button></div>
        {:else if result?.in_sync}
          <div class="state ok" role="status"><Check size={22} aria-hidden="true" /><p>Schema is in sync</p><small>{result.quick ? 'Nothing changed since the last publish.' : 'A publish would make no changes.'}</small></div>
        {:else if result?.file_changes?.length}
          <div class="summary">
            <p class="headline">{result.file_changes.length} source file{result.file_changes.length === 1 ? '' : 's'} changed since the last publish</p>
            <section class="group">
              <ul>
                {#each result.file_changes as c (`${c.action}|${c.path}`)}
                  <li>
                    <span class="type">{c.action}</span>
                    <span class="name">{c.path}</span>
                  </li>
                {/each}
              </ul>
            </section>
            <div class="fast-actions"><p class="fast-note">Analyze the database impact to see affected objects and data-loss warnings.</p><button class="primary-action" aria-busy={loading} onclick={() => void loadSummary('analyze')}>Analyze database impact</button></div>
          </div>
        {:else if result}
          <div class="summary">
            <p class="headline">A publish would apply
              {#if result.created}<span class="count create">+{result.created} create</span>{/if}
              {#if result.altered}<span class="count alter">~{result.altered} alter</span>{/if}
              {#if result.dropped}<span class="count drop">−{result.dropped} drop</span>{/if}
              {#if result.cached}<span class="fast-note">(previously analyzed)</span>{/if}
            </p>

            {#if result.data_loss}
              <div class="alerts" role="alert">
                <div class="alerts-head"><TriangleAlert size={15} aria-hidden="true" /> Possible data loss</div>
                <ul>{#each result.alerts as a (a.message)}<li>{a.message}</li>{/each}</ul>
              </div>
            {/if}

            {#each (['Drop', 'Alter', 'Create'] as const) as action (action)}
              {#if grouped[action].length}
                <section class="group group-{action.toLowerCase()}">
                  <h3>{action} <span class="n">{grouped[action].length}</span></h3>
                  <ul>
                    {#each grouped[action] as op (`${op.object_type}|${op.name}`)}
                      <li class:lossy={op.data_loss}>
                        <span class="type">{shortType(op.object_type)}</span>
                        <span class="name">{op.name}</span>
                        {#if op.data_loss}<span class="loss-tag"><TriangleAlert size={11} aria-hidden="true" /> data loss</span>{/if}
                      </li>
                    {/each}
                  </ul>
                </section>
              {/if}
            {/each}
          </div>
        {/if}
      {:else}
        {#if scriptLoading}
          <div class="state" role="status"><Loader2 size={22} class="spin" aria-hidden="true" /><p>Generating deployment script…</p></div>
        {:else if scriptError}
          <div class="state err" role="alert"><TriangleAlert size={22} aria-hidden="true" /><p>{scriptError}</p><button class="retry" onclick={() => void loadScript()}>Retry</button></div>
        {:else if script !== null}
          {#if script.trim() === ''}
            <div class="state ok" role="status"><Check size={22} aria-hidden="true" /><p>No script — schema is in sync</p></div>
          {:else}
            <pre class="script">{script}</pre>
          {/if}
        {/if}
      {/if}
    </div>
  </div>
</div>

<style>
  .backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.7); z-index: 1000; display: flex; align-items: center; justify-content: center; }
  .modal { width: 90vw; max-width: 900px; height: 82vh; background: var(--card); border: 1px solid var(--border); border-radius: var(--radius-lg); display: flex; flex-direction: column; overflow: hidden; }
  header { display: flex; align-items: center; gap: var(--space-3); padding: var(--space-2) var(--space-3); border-bottom: 1px solid var(--border); background: var(--bg); }
  .title { font-size: var(--text-md); color: var(--text-secondary); }
  .title strong { color: var(--fg); font-family: var(--font-mono); }
  .tabs { display: inline-flex; gap: 2px; padding: 2px; border: 1px solid var(--border); border-radius: var(--radius-sm); margin-left: auto; }
  .tabs button { background: none; border: 0; color: var(--dim); font-size: var(--text-sm); padding: 3px 12px; border-radius: 3px; cursor: pointer; font-family: inherit; }
  .tabs button.active { background: color-mix(in srgb, var(--blue) 18%, transparent); color: var(--fg); }
  .close { background: none; border: 1px solid var(--border); color: var(--fg); border-radius: var(--radius-sm); cursor: pointer; width: 28px; height: 28px; display: inline-flex; align-items: center; justify-content: center; }
  .close:hover { background: var(--blue); color: var(--white); }
  .body { flex: 1; min-height: 0; overflow: auto; padding: var(--space-4); }

  .state { height: 100%; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: var(--space-2); color: var(--text-secondary); text-align: center; }
  .state p { margin: 0; font-size: var(--text-md); color: var(--fg); }
  .state small { color: var(--dim); }
  .state.ok :global(svg) { color: var(--green); }
  .state.err :global(svg) { color: var(--red); }
  .state.err p { color: var(--red); }
  .retry { margin-top: var(--space-2); background: var(--card); border: 1px solid var(--border); color: var(--fg); border-radius: var(--radius-sm); padding: 4px 14px; cursor: pointer; }
  .primary-action { margin-top: var(--space-2); display: inline-flex; align-items: center; border-radius: var(--radius-sm); padding: 5px 14px; cursor: pointer; }
  .primary-action { border: 1px solid var(--blue); background: color-mix(in srgb, var(--blue) 18%, transparent); color: var(--fg); }

  .summary { display: flex; flex-direction: column; gap: var(--space-4); }
  .headline { margin: 0; display: flex; flex-wrap: wrap; gap: var(--space-2); align-items: center; color: var(--text-secondary); }
  .count { font-family: var(--font-mono); font-size: var(--text-sm); padding: 1px 8px; border-radius: var(--radius-pill); }
  .count.create { background: color-mix(in srgb, var(--green) 18%, transparent); color: var(--green); }
  .count.alter { background: color-mix(in srgb, var(--yellow) 18%, transparent); color: var(--yellow); }
  .count.drop { background: color-mix(in srgb, var(--red) 18%, transparent); color: var(--red); }
  .fast-note { margin: 0; color: var(--text-tertiary, var(--text-secondary)); font-size: var(--text-sm); }
  .fast-actions { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); flex-wrap: wrap; }
  .fast-actions .primary-action { margin-top: 0; }

  .alerts { border: 1px solid color-mix(in srgb, var(--red) 45%, var(--border)); border-radius: var(--radius); background: color-mix(in srgb, var(--red) 8%, transparent); padding: var(--space-2) var(--space-3); }
  .alerts-head { display: flex; align-items: center; gap: var(--space-1); color: var(--red); font-weight: 600; font-size: var(--text-sm); }
  .alerts-head :global(svg) { color: var(--red); }
  .alerts ul { margin: var(--space-1) 0 0; padding-left: var(--space-5); color: var(--text-secondary); font-size: var(--text-sm); }

  .group h3 { margin: 0 0 var(--space-1); font-size: var(--text-sm); text-transform: uppercase; letter-spacing: 0.03em; display: flex; align-items: center; gap: var(--space-2); }
  .group-drop h3 { color: var(--red); }
  .group-alter h3 { color: var(--yellow); }
  .group-create h3 { color: var(--green); }
  .group h3 .n { background: color-mix(in srgb, var(--fg) 10%, transparent); color: var(--text-secondary); border-radius: var(--radius-pill); padding: 0 7px; font-size: var(--text-xs); }
  .group ul { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
  .group li { display: flex; align-items: center; gap: var(--space-2); padding: 3px var(--space-2); border-radius: var(--radius-sm); font-size: var(--text-sm); }
  .group li.lossy { background: color-mix(in srgb, var(--red) 8%, transparent); }
  .group .type { color: var(--dim); font-size: var(--text-xs); min-width: 84px; }
  .group .name { font-family: var(--font-mono); }
  .loss-tag { display: inline-flex; align-items: center; gap: 3px; margin-left: auto; color: var(--red); font-size: var(--text-xs); }
  .loss-tag :global(svg) { color: var(--red); }

  .script { margin: 0; font-family: var(--font-mono); font-size: var(--text-sm); white-space: pre; color: var(--fg); }

  :global(.spin) { animation: spin 1s linear infinite; }
  @media (prefers-reduced-motion: reduce) { :global(.spin) { animation: none; } }
  @keyframes spin { to { transform: rotate(360deg); } }
</style>
