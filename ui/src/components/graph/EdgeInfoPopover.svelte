<script lang="ts">
  import { store } from '../../lib/stores.svelte'
  import { optimisticDetach } from '../../lib/graphActions'
  import { X } from '@lucide/svelte'
  import { computePosition, autoUpdate, offset, flip, shift, arrow } from '@floating-ui/dom'
  import { onClickOutside } from 'runed'

  const edge = $derived(
    store.graph.selectedEdge && store.graph.data
      ? store.graph.data.edges.find(
          e => e.from === store.graph.selectedEdge!.from && e.to === store.graph.selectedEdge!.to
        )
      : null
  )

  let popoverEl: HTMLDivElement | null = $state(null)
  let arrowEl: HTMLDivElement | null = $state(null)
  let cleanup: (() => void) | null = null

  $effect(() => {
    if (!edge || !store.graph.selectedEdge || !popoverEl || !arrowEl) {
      cleanup?.()
      cleanup = null
      return
    }
    const cx = store.graph.selectedEdge.x
    const cy = store.graph.selectedEdge.y
    const virtualRef = {
      getBoundingClientRect: () => ({
        x: cx, y: cy, top: cy, left: cx, right: cx, bottom: cy, width: 0, height: 0,
        toJSON: () => ({}),
      }),
    }
    const pop = popoverEl
    const arr = arrowEl
    cleanup = autoUpdate(virtualRef as any, pop, () => {
      computePosition(virtualRef as any, pop, {
        placement: 'top',
        middleware: [offset(12), flip({ padding: 16 }), shift({ padding: 16 }), arrow({ element: arr })],
      }).then(({ x, y, placement, middlewareData }) => {
        pop.style.left = `${x}px`
        pop.style.top = `${y}px`
        pop.dataset.placement = placement
        const a = middlewareData.arrow
        if (a) {
          const side = placement.split('-')[0]
          const opp = { top: 'bottom', bottom: 'top', left: 'right', right: 'left' }[side]!
          Object.assign(arr.style, {
            left: a.x != null ? `${a.x}px` : '',
            top:  a.y != null ? `${a.y}px` : '',
            [opp]: '-5px',
          })
        }
      })
    })
    return () => { cleanup?.(); cleanup = null }
  })

  function close() {
    store.graph.selectedEdge = null
  }

  function handleKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && edge) close()
  }

  // Dismiss when the user clicks anywhere outside the popover. Note: the
  // original edge click (which opened the popover) is on the SvelteFlow
  // edge element, which is outside this popover — but onClickOutside is
  // attached *after* the popover mounts, so the opening click is over by
  // then and doesn't trigger an immediate dismiss.
  onClickOutside(() => popoverEl, () => {
    if (edge) close()
  })

  async function toggleDetach() {
    if (!edge) return
    await optimisticDetach(edge, !edge.detached)
  }
</script>

<svelte:window onkeydown={handleKey} />

{#if edge}
  <div
    bind:this={popoverEl}
    class="popover"
    role="dialog"
    aria-labelledby="edge-title"
  >
    <header>
      <h3 id="edge-title">{edge.from} → {edge.to}</h3>
      <button type="button" aria-label="Close" onclick={close}><X size={18} /></button>
    </header>
    {#if edge.env_vars && edge.env_vars.length > 0}
      <p class="hint">Connection wired by these env vars:</p>
      <ul>
        {#each edge.env_vars as v (v)}<li><code>{v}</code></li>{/each}
      </ul>
    {:else}
      <p class="hint">No auto-injected env vars for this dependency.</p>
    {/if}

    {#if edge.detachable}
      <footer>
        <button
          type="button"
          class="detach-btn"
          class:detached={edge.detached}
          onclick={toggleDetach}
        >{edge.detached ? 'Reattach' : 'Detach'}</button>
      </footer>
    {:else}
      <p class="hint dim">Only frontend → backend edges can be detached.</p>
    {/if}

    <div bind:this={arrowEl} class="popover-arrow"></div>
  </div>
{/if}

<style>
  .popover {
    position: absolute;
    top: 0;
    left: 0;
    background: var(--card);
    border: 1px solid var(--border);
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius-lg);
    min-width: 280px;
    max-width: 460px;
    z-index: 60;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
  }
  .popover-arrow {
    position: absolute;
    width: 10px;
    height: 10px;
    background: var(--card);
    border: 1px solid var(--border);
    transform: rotate(45deg);
  }
  :global(.popover[data-placement^="top"]) .popover-arrow {
    border-top: 0;
    border-left: 0;
  }
  :global(.popover[data-placement^="bottom"]) .popover-arrow {
    border-bottom: 0;
    border-right: 0;
  }
  :global(.popover[data-placement^="left"]) .popover-arrow {
    border-bottom: 0;
    border-left: 0;
  }
  :global(.popover[data-placement^="right"]) .popover-arrow {
    border-top: 0;
    border-right: 0;
  }
  header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  h3 {
    margin: 0;
    font-family: monospace;
    font-size: var(--text-lg);
    color: var(--fg);
  }
  button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: 0;
    color: var(--dim);
    cursor: pointer;
    padding: 0;
    width: var(--space-5);
    height: var(--space-5);
    line-height: 0;
  }
  button:hover { color: var(--fg); }
  .hint {
    color: var(--dim);
    font-size: var(--text-md);
    margin: var(--space-2) 0 var(--space-1) 0;
  }
  ul {
    margin: 0;
    padding-left: 18px;
    list-style: disc;
  }
  li {
    font-size: var(--text-md);
    color: var(--fg);
    margin-bottom: var(--space-1);
  }
  code {
    font-family: monospace;
    font-size: var(--text-md);
    color: var(--blue);
  }
  footer {
    display: flex;
    justify-content: flex-end;
    margin-top: var(--space-3);
  }
  .detach-btn {
    width: auto;
    height: auto;
    padding: var(--space-1) var(--space-3);
    border: 1px solid var(--red);
    color: var(--red);
    border-radius: var(--radius-sm);
    background: transparent;
    font-family: inherit;
    font-size: var(--text-md);
  }
  .detach-btn:hover {
    background: color-mix(in srgb, var(--red) 12%, transparent);
    color: var(--red);
  }
  .detach-btn.detached {
    border-color: var(--blue);
    color: var(--blue);
  }
  .detach-btn.detached:hover {
    background: color-mix(in srgb, var(--blue) 12%, transparent);
    color: var(--blue);
  }
  .hint.dim {
    font-size: var(--text-sm);
    color: var(--dim);
    margin-top: var(--space-3);
    margin-bottom: 0;
  }
</style>
