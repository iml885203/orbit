<script lang="ts">
  import { LayoutGrid, List, Share2, StretchHorizontal } from '@lucide/svelte'
  import { store } from '$lib/stores.svelte'
  import { tooltip } from '$lib/tooltip.svelte'

  let { hasGroups }: { hasGroups: boolean } = $props()
</script>

<div class="toolbar" aria-label="Services graph controls">
  {#if hasGroups}
    <div class="toggle" role="group" aria-label="Group layout">
      <button
        class:active={store.ui.layoutMode === 'rectangle'}
        aria-pressed={store.ui.layoutMode === 'rectangle'}
        aria-label="Rectangle layout"
        use:tooltip={{ content: 'Rectangle — compact grid' }}
        onclick={() => store.ui.setLayoutMode('rectangle')}
      >
        <LayoutGrid size={15} />
      </button>
      <button
        class:active={store.ui.layoutMode === 'extend'}
        aria-pressed={store.ui.layoutMode === 'extend'}
        aria-label="Extended layout"
        use:tooltip={{ content: 'Extend — wide dependency rows' }}
        onclick={() => store.ui.setLayoutMode('extend')}
      >
        <StretchHorizontal size={15} />
      </button>
    </div>
  {/if}
  <div class="toggle" role="group" aria-label="Services view">
    <button
      class:active={store.ui.serviceView === 'graph'}
      aria-pressed={store.ui.serviceView === 'graph'}
      aria-label="Graph view"
      use:tooltip={{ content: 'Graph view' }}
      onclick={() => store.ui.setServiceView('graph')}
    >
      <Share2 size={15} />
    </button>
    <button
      class:active={store.ui.serviceView === 'table'}
      aria-pressed={store.ui.serviceView === 'table'}
      aria-label="Table view"
      use:tooltip={{ content: 'Table view' }}
      onclick={() => store.ui.setServiceView('table')}
    >
      <List size={15} />
    </button>
  </div>
</div>

<style>
  .toolbar { display: flex; gap: var(--space-3); }
  .toggle { display: inline-flex; gap: 2px; padding: 2px; border: 1px solid var(--border); border-radius: var(--radius-md); background: var(--card); }
  button { display: inline-flex; align-items: center; justify-content: center; width: 28px; height: 28px; padding: 0; border: 0; border-radius: var(--radius-sm); background: transparent; color: var(--dim); cursor: pointer; }
  button:hover { color: var(--fg); background: color-mix(in srgb, var(--fg) 8%, transparent); }
  button.active { background: color-mix(in srgb, var(--blue) 16%, transparent); color: var(--blue); }
</style>
