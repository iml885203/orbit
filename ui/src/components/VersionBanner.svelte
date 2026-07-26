<script lang="ts">
  import { store, toast } from '$lib/stores.svelte'
  import { tooltip } from '../lib/tooltip.svelte'

  const cmd = 'orbit daemon restart'

  async function copyCmd() {
    try {
      await navigator.clipboard.writeText(cmd)
      toast('Command copied — paste into terminal')
    } catch {
      toast('Copy failed — ' + cmd)
    }
  }
</script>

{#if store.ui.version?.update_available}
  <div class="banner">
    <span class="text">
      {#if store.ui.version.on_disk_path}
        New orbit at <code>{store.ui.version.on_disk_path}</code>: <code>{store.ui.version.on_disk}</code> — daemon is on <code>{store.ui.version.running}</code>. Restart to pick it up.
      {:else}
        New build available: <code>{store.ui.version.on_disk}</code> — daemon is still on <code>{store.ui.version.running}</code>. Restart to pick it up.
      {/if}
    </span>
    <button use:tooltip={{ content: 'Copy restart command' }} onclick={copyCmd}>Copy restart</button>
  </div>
{/if}

<style>
  .banner {
    display: flex;
    align-items: center;
    gap: 0.6rem; /* off-grid: intentional compact banner rhythm */
    padding: 0.35rem var(--space-4); /* 0.35rem off-grid vertical */
    background: rgba(210, 153, 34, 0.1);
    color: var(--yellow);
    border-bottom: 1px solid rgba(210, 153, 34, 0.25);
    font-size: var(--text-md);
  }
  .text { flex: 1; color: var(--dim); }
  .text :global(code) {
    color: var(--yellow);
    background: rgba(210, 153, 34, 0.12);
    padding: 0.05rem 0.3rem; /* off-grid: intentional tight inline code padding */
    border-radius: 3px; /* off-grid: between 0 and radius-sm */
    font-family: ui-monospace, monospace;
    font-size: var(--text-md);
  }
  button {
    background: transparent;
    color: var(--dim);
    border: 1px solid var(--border);
    padding: 0.15rem 0.55rem; /* off-grid: intentional compact banner button */
    border-radius: 3px; /* off-grid: between 0 and radius-sm */
    cursor: pointer;
    font-size: var(--text-md);
  }
  button:hover { color: var(--fg); border-color: var(--dim); }
</style>
