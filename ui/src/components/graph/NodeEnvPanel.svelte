<script lang="ts">
  import type { GraphNode, EnvVarEntry } from '../../lib/types.gen'
  import { store } from '../../lib/stores.svelte'
  import { fetchServiceEnv } from '../../lib/api'
  import { tooltip } from '../../lib/tooltip.svelte'
  import { SvelteMap } from 'svelte/reactivity'

  let { node, onNavigate }: { node: GraphNode | null; onNavigate: (depName: string) => void } = $props()

  let envEntries = $state<EnvVarEntry[]>([])
  let envLoading = $state(false)

  // Group env entries by source for easier scanning.
  const envGroups = $derived.by(() => {
    const groups: Record<string, EnvVarEntry[]> = { explicit: [], toggle: [], dependency: [] }
    for (const e of envEntries) {
      ;(groups[e.source] ?? groups.explicit).push(e)
    }
    return groups
  })

  // Nest dependency entries by the container they came from.
  // Returns [{ name, entries[] }] in dep order; vars without a dependency
  // tag fall under "(unknown)".
  const dependencyBuckets = $derived.by(() => {
    const buckets = new SvelteMap<string, EnvVarEntry[]>()
    for (const e of envGroups.dependency ?? []) {
      const k = e.dependency || '(unknown)'
      const arr = buckets.get(k)
      if (arr) arr.push(e)
      else buckets.set(k, [e])
    }
    return Array.from(buckets, ([name, entries]) => ({ name, entries }))
  })

  // Map a toggle-sourced env key back to the toggle's human-readable label
  // for tooltip display. Returns null when not found (or when the entry
  // isn't toggle-sourced).
  function toggleLabelFor(key: string): string | null {
    const t = store.daemon.envToggles.find(t => t.service === node?.name && t.var === key)
    return t ? `Toggle: ${t.label}` : null
  }

  function isNavigable(depName: string): boolean {
    return !!store.graph.data?.nodes.find(n => n.name === depName)
  }

  // Only refetch when the *selected node identity* changes. node itself
  // gets a fresh object reference on every SSE tick (store.graph is
  // re-assigned), so depending on `node` directly causes a refetch every
  // poll — which clears envEntries and resets the drawer scroll. Keying on
  // node.name keeps the effect stable while the same node is open.
  let lastFetchedFor = $state<string | null>(null)
  $effect(() => {
    const key = node && node.kind !== 'infra' ? node.name : null
    if (key === lastFetchedFor) return
    lastFetchedFor = key
    if (key === null) {
      envEntries = []
      return
    }
    envLoading = true
    fetchServiceEnv(key).then(resp => {
      // Guard against race: drawer may have moved to a different node
      // by the time the fetch resolves.
      if (lastFetchedFor !== key) return
      envEntries = resp?.env ?? []
      envLoading = false
    })
  })
</script>

{#if envLoading}
  <p class="dim">Loading…</p>
{:else if envEntries.length === 0}
  <p class="dim">No injected env vars.</p>
{:else}
  {#if envGroups.explicit.length}
    <h4 class="env-group source-explicit">From config</h4>
    <ul class="env-list source-explicit">
      {#each envGroups.explicit as e (e.key)}
        <li>
          <span class="env-key">{e.key}</span>
          <code class="env-value">{e.value}</code>
        </li>
      {/each}
    </ul>
  {/if}

  {#if dependencyBuckets.length}
    <h4 class="env-group source-dependency">From dependencies</h4>
    {#each dependencyBuckets as bucket (bucket.name)}
      <div class="dep-bucket">
        <div class="dep-bucket-name">
          {#if isNavigable(bucket.name)}
            <button class="dep-nav-inline" type="button" onclick={() => onNavigate(bucket.name)}>{bucket.name}</button>
          {:else}
            {bucket.name}
          {/if}
        </div>
        <ul class="env-list source-dependency">
          {#each bucket.entries as e (e.key)}
            <li>
              <span class="env-key">{e.key}</span>
              <code class="env-value">{e.value}</code>
            </li>
          {/each}
        </ul>
      </div>
    {/each}
  {/if}

  {#if envGroups.toggle.length}
    <h4 class="env-group source-toggle">Toggleable</h4>
    <ul class="env-list source-toggle">
      {#each envGroups.toggle as e (e.key)}
        <li use:tooltip={{ content: toggleLabelFor(e.key) ?? '' }}>
          <span class="env-key">{e.key}</span>
          <code class="env-value">{e.value}</code>
        </li>
      {/each}
    </ul>
  {/if}
{/if}

<style>
  .dim { color: var(--dim); font-size: var(--text-md); }

  .env-group {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: var(--space-3) 0 var(--space-1);
    font-size: var(--text-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }
  .env-group.source-explicit   { color: var(--blue); }
  .env-group.source-toggle     { color: var(--kind-backend); }
  .env-group.source-dependency { color: var(--dim); }

  .dep-bucket {
    margin-bottom: var(--space-3);
  }
  .dep-bucket-name {
    font-family: monospace;
    font-size: var(--text-md);
    color: var(--dim);
    margin: var(--space-2) 0 var(--space-1);
    padding-left: var(--space-2);
    border-left: 2px solid var(--dim);
  }
  .dep-nav-inline {
    background: transparent;
    border: 0;
    padding: 0;
    color: var(--dim);
    cursor: pointer;
    font-family: monospace;
    font-size: var(--text-md);
    text-decoration: underline;
    text-decoration-color: transparent;
    text-underline-offset: 3px;
    transition: color var(--transition-fast), text-decoration-color var(--transition-fast);
  }
  .dep-nav-inline:hover {
    color: var(--blue);
    text-decoration-color: var(--blue);
  }
  .env-list { list-style: none; padding: 0; margin: 0 0 var(--space-3); font-family: monospace; font-size: var(--text-md); }
  .env-list li {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: var(--space-2) 0;
    border-bottom: 1px dotted var(--border);
  }
  .env-key { color: var(--fg); font-weight: 500; }
  .env-value {
    color: var(--dim);
    word-break: break-all;
    font-size: var(--text-sm);
  }
</style>
