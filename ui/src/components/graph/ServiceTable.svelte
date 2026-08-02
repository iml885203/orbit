<script lang="ts">
  import { ExternalLink, Play, RotateCcw, ScrollText, Search, Square } from '@lucide/svelte'
  import type { GraphNode, GraphResponse } from '../../lib/types.gen'
  import { apiPost } from '../../lib/api'
  import { COLORS, ICONS } from '../../lib/constants'
  import { isRunning, mutationsDisabled, openLogViewer, toast } from '../../lib/stores.svelte'
  import { tooltip } from '../../lib/tooltip.svelte'
  import { SvelteMap, SvelteSet } from 'svelte/reactivity'

  let { graph, onSelect }: { graph: GraphResponse; onSelect: (name: string) => void } = $props()

  let query = $state('')
  const busy = new SvelteSet<string>()
  const readOnly = $derived(mutationsDisabled())
  const groupOf = $derived.by(() => {
    const groups = new SvelteMap<string, string>()
    for (const group of graph.groups ?? []) {
      for (const service of group.services) groups.set(service, group.name)
    }
    return groups
  })
  const dependencies = $derived.by(() => {
    const result = new SvelteMap<string, string[]>()
    for (const edge of graph.edges) {
      if (edge.kind !== 'sync') continue
      const values = result.get(edge.from) ?? []
      values.push(edge.to)
      result.set(edge.from, values)
    }
    return result
  })
  const filtered = $derived.by(() => {
    const needle = query.trim().toLowerCase()
    if (!needle) return graph.nodes
    return graph.nodes.filter(node => [
      node.name, node.kind, node.state, node.mode ?? '', groupOf.get(node.name) ?? '',
    ].some(value => value.toLowerCase().includes(needle)))
  })

  function selectRow(event: MouseEvent, name: string): void {
    if ((event.target as Element).closest('button, a')) return
    onSelect(name)
  }

  async function run(name: string, request: () => Promise<{ ok: boolean; data?: { error?: string } }>): Promise<void> {
    if (busy.has(name)) return
    busy.add(name)
    try {
      const { ok, data } = await request()
      if (!ok) toast(data?.error || `Failed to update ${name}`)
    } finally {
      busy.delete(name)
    }
  }

  function portSummary(node: GraphNode): string {
    return Object.entries(node.ports ?? {}).map(([name, port]) => `${name}:${port}`).join(' · ')
  }
</script>

<section class="service-table" aria-label="Service table">
  <div class="table-tools">
    <label class="search">
      <Search size={14} aria-hidden="true" />
      <span class="sr-only">Filter services</span>
      <input bind:value={query} type="search" placeholder="Filter services" />
    </label>
    <span class="count">{filtered.length} of {graph.nodes.length}</span>
  </div>

  <div class="table-scroll">
    <table>
      <thead>
        <tr>
          <th scope="col">State</th>
          <th scope="col">Resource</th>
          <th scope="col">Group</th>
          <th scope="col">Type</th>
          <th scope="col">Endpoint</th>
          <th scope="col">Dependencies</th>
          <th scope="col" class="actions-heading">Actions</th>
        </tr>
      </thead>
      <tbody>
        {#each filtered as node (node.name)}
          {@const deps = dependencies.get(node.name) ?? []}
          {@const running = isRunning(node.state)}
          {@const pending = busy.has(node.name)}
          <tr onclick={(event) => selectRow(event, node.name)} tabindex="0" onkeydown={(event) => {
            if (event.target === event.currentTarget && (event.key === 'Enter' || event.key === ' ')) { event.preventDefault(); onSelect(node.name) }
          }}>
            <td>
              <span class="state" style:color={COLORS[node.state]} title={node.stateReason ?? node.state}>
                <span aria-hidden="true">{ICONS[node.state] ?? '?'}</span>
                <span>{node.state}</span>
              </span>
            </td>
            <td><strong class="resource-name">{node.label ?? node.name}</strong></td>
            <td class="muted">{groupOf.get(node.name) ?? '—'}</td>
            <td><span class="kind kind-{node.kind}">{node.kind}</span>{#if node.mode}<span class="mode">{node.mode}</span>{/if}</td>
            <td class="endpoint">
              {#if !readOnly && node.url && node.state === 'healthy'}
                <a href={node.url} target="_blank" rel="noreferrer">{portSummary(node) || node.url}</a>
              {:else}
                <span class="muted">{portSummary(node) || '—'}</span>
              {/if}
            </td>
            <td>
              {#if deps.length}
                <div class="deps" title={deps.join(', ')}>
                  {#each deps.slice(0, 2) as dep (dep)}<span>{dep}</span>{/each}
                  {#if deps.length > 2}<span>+{deps.length - 2}</span>{/if}
                </div>
              {:else}<span class="muted">—</span>{/if}
            </td>
            <td>
              <div class="actions">
                {#if node.kind !== 'external'}
                  {#if !running && !node.portConflict}
                    <button aria-label="Start {node.name}" use:tooltip={{ content: 'Start' }} disabled={readOnly || pending} onclick={() => run(node.name, () => apiPost('/api/up', { resources: [node.name] }))}><Play size={14} /></button>
                  {:else if running && !node.portConflict && !node.blockedBy}
                    <button aria-label="Restart {node.name}" use:tooltip={{ content: 'Restart' }} disabled={readOnly || pending} onclick={() => run(node.name, () => apiPost(`/api/restart/${node.name}`))}><RotateCcw size={14} /></button>
                    <button class="danger" aria-label="Stop {node.name}" use:tooltip={{ content: 'Stop' }} disabled={readOnly || pending} onclick={() => run(node.name, () => apiPost(`/api/stop/${node.name}`))}><Square size={13} fill="currentColor" /></button>
                  {/if}
                {/if}
                {#if !readOnly && node.url && node.state === 'healthy'}
                  <a class="icon-action" href={node.url} target="_blank" rel="noreferrer" aria-label="Open {node.name} in browser" use:tooltip={{ content: 'Open in browser' }}><ExternalLink size={14} /></a>
                {/if}
                {#if !readOnly && (node.logsAvailable || node.kind !== 'external')}
                  <button aria-label="Open logs for {node.name}" use:tooltip={{ content: 'Logs' }} onclick={() => openLogViewer(node.name)}><ScrollText size={14} /></button>
                {/if}
              </div>
            </td>
          </tr>
        {:else}
          <tr><td colspan="7" class="empty">No services match “{query}”.</td></tr>
        {/each}
      </tbody>
    </table>
  </div>
</section>

<style>
  .service-table { display: flex; flex: 1 1 auto; flex-direction: column; min-height: 0; background: var(--bg); }
  .table-tools { display: flex; align-items: center; gap: var(--space-3); padding: var(--space-2) var(--space-4); border-bottom: 1px solid var(--border); }
  .search { display: flex; align-items: center; gap: var(--space-2); width: 280px; height: 30px; padding: 0 var(--space-2); border: 1px solid var(--border); border-radius: var(--radius-md); color: var(--dim); background: var(--card); }
  .search:focus-within { border-color: var(--blue); }
  .search input { width: 100%; border: 0; outline: 0; padding: 0; color: var(--fg); background: transparent; font: inherit; }
  .count { color: var(--dim); font-size: var(--text-sm); }
  .table-scroll { min-height: 0; overflow: auto; }
  table { width: 100%; border-collapse: collapse; font-size: var(--text-md); }
  thead { position: sticky; top: 0; z-index: 2; background: var(--card); }
  th { height: 34px; padding: 0 var(--space-3); border-bottom: 1px solid var(--border); color: var(--dim); font-size: var(--text-xs); font-weight: 600; text-align: left; text-transform: uppercase; letter-spacing: 0.04em; }
  td { height: 48px; padding: var(--space-2) var(--space-3); border-bottom: 1px solid var(--border); color: var(--fg); vertical-align: middle; }
  tbody tr { cursor: pointer; }
  tbody tr:hover, tbody tr:focus-visible { background: color-mix(in srgb, var(--blue) 6%, transparent); outline: none; }
  .state { display: inline-flex; align-items: center; gap: var(--space-2); font-family: var(--font-mono); }
  .resource-name, .endpoint { font-family: var(--font-mono); font-size: var(--text-sm); }
  .muted { color: var(--dim); }
  .kind, .mode, .deps span { display: inline-flex; padding: 2px var(--space-1); border-radius: var(--radius-sm); border: 1px solid var(--border); color: var(--dim); font-size: var(--text-xs); }
  .kind-frontend { color: var(--kind-frontend); }.kind-backend { color: var(--kind-backend); }.kind-infra { color: var(--kind-infra); }
  .mode { margin-left: var(--space-1); }
  .endpoint a { color: var(--blue); text-decoration: none; }
  .endpoint a:hover { text-decoration: underline; }
  .deps { display: flex; gap: var(--space-1); max-width: 280px; overflow: hidden; }
  .deps span { max-width: 110px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .actions-heading { text-align: right; }
  .actions { display: flex; justify-content: flex-end; gap: var(--space-1); }
  .actions button, .icon-action { display: inline-flex; align-items: center; justify-content: center; width: 28px; height: 28px; padding: 0; border: 1px solid transparent; border-radius: var(--radius-md); background: transparent; color: var(--dim); cursor: pointer; }
  .actions button:hover:not(:disabled), .icon-action:hover { border-color: var(--border); background: var(--card); color: var(--fg); }
  .actions .danger:hover:not(:disabled) { color: var(--red); }
  .actions button:disabled { opacity: 0.4; cursor: not-allowed; }
  .empty { padding: var(--space-5); color: var(--dim); text-align: center; }
  .sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; }
</style>
