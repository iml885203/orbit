<script lang="ts">
  import { push } from 'svelte-spa-router'
  import { Eye } from '@lucide/svelte'
  import { store, toast } from '$lib/stores.svelte'
  import { fetchGraph } from '$lib/api'
  import { envShortName } from '$lib/envName'

  function close() {
    store.ui.envPopoverOpen = false
  }

  async function preview(envName: string) {
    const shortName = envShortName(envName)
    if (shortName === store.graph.data?.env) {
      store.graph.preview = null
      close()
      await push('/')
      return
    }
    const graph = await fetchGraph(shortName)
    if (!graph) {
      toast(`Failed to load ${shortName}`)
      return
    }
    store.graph.preview = graph
    close()
    await push('/')
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
    <ul>
      {#each store.daemon.envs?.envs ?? [] as env (env.path)}
        {@const short = envShortName(env.name)}
        {@const previewing = store.graph.preview?.env === short}
        <li>
          <button
            class="env-row"
            class:current={env.current}
            class:previewing
            onclick={() => preview(env.name)}
            title={env.path}
          >
            <span class="dot" class:active={env.current}></span>
            <span class="name">{short}</span>
            {#if env.current}<span class="badge">current</span>{/if}
            {#if previewing}<span class="badge preview"><Eye size={11} aria-hidden="true" /> preview</span>{/if}
          </button>
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
    z-index: 9;
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
    z-index: 10;
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
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 280px;
    overflow-y: auto;
  }
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
