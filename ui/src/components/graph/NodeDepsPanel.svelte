<script lang="ts">
  import type { GraphNode } from '../../lib/types.gen'
  import { store } from '../../lib/stores.svelte'
  import { optimisticDetach } from '../../lib/graphActions'

  let { node, onNavigate, readOnly = false }: { node: GraphNode | null; onNavigate: (depName: string) => void; readOnly?: boolean } = $props()

  // Read the same active graph as the canvas so previewed nodes show
  // their deps. preview-wins lives on the store.
  const activeGraph = $derived(store.graph.active)

  // Outgoing edges where `from` is this node.
  const outgoing = $derived(
    node && activeGraph
      ? activeGraph.edges.filter(e => e.from === node.name)
      : []
  )

  function isNavigable(depName: string): boolean {
    return !!activeGraph?.nodes.find(n => n.name === depName)
  }
</script>

{#if outgoing.length > 0}
  <ul class="deps">
    {#each outgoing as e (e.from + '->' + e.to)}
      <li>
        {#if isNavigable(e.to)}
          <button class="dep-name dep-nav" type="button" onclick={() => onNavigate(e.to)}>
            {e.to}
          </button>
        {:else}
          <span class="dep-name">{e.to}</span>
        {/if}
        {#if e.detachable}
          <button type="button" disabled={readOnly} onclick={() => optimisticDetach(e, !e.detached)}>
            {e.detached ? 'Reattach' : 'Detach'}
          </button>
        {:else}
          <span class="dim">(not detachable)</span>
        {/if}
      </li>
    {/each}
  </ul>
{/if}

<style>
  .deps { list-style: none; padding: 0; margin: var(--space-2) 0; }
  .deps li {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: var(--space-2) 0;
    border-bottom: 1px dotted var(--border);
  }
  .dim { color: var(--dim); font-size: var(--text-md); }
  .deps button {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg);
    padding: 2px var(--space-3);
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-family: monospace;
    font-size: var(--text-md);
  }
  .deps button:hover:not(:disabled) { border-color: var(--blue); color: var(--blue); }
  .deps button:disabled { opacity: 0.5; cursor: not-allowed; }
  .dep-name { color: var(--fg); font-family: monospace; font-size: var(--text-md); }
  button.dep-nav {
    background: transparent;
    border: 0;
    padding: 0;
    cursor: pointer;
    text-align: left;
    text-decoration: underline;
    text-decoration-color: transparent;
    text-underline-offset: 3px;
    transition: color var(--transition-fast), text-decoration-color var(--transition-fast);
  }
  button.dep-nav:hover {
    color: var(--blue);
    text-decoration-color: var(--blue);
  }
</style>
