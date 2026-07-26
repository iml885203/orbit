<script lang="ts">
  import { Copy, ChevronUp, ChevronDown, CheckCircle2, Clock3, XCircle } from '@lucide/svelte'
  import { history } from '$lib/history.svelte'
  import { store, toast } from '$lib/stores.svelte'
  import HistoryPanel from './HistoryPanel.svelte'
  import { tooltip } from '$lib/tooltip.svelte'

  const latest = $derived(history.latest)

  function textFor(rec = latest) {
    return rec?.command || rec?.summary || ''
  }

  async function copyLatest(e: MouseEvent) {
    e.stopPropagation()
    const text = textFor()
    if (!text) return
    await navigator.clipboard.writeText(text)
    toast('Copied command')
  }

  function iconFor(status: string | undefined) {
    if (status === 'ok') return CheckCircle2
    if (status === 'error') return XCircle
    return Clock3
  }

  function toggleFromKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      history.expanded = !history.expanded
    }
  }
</script>

{#if store.ui.showHistory}
  {#if history.expanded}
    <HistoryPanel onclose={() => (history.expanded = false)} />
  {/if}

  <div
    class="history-bar"
    role="button"
    tabindex="0"
    onclick={() => (history.expanded = !history.expanded)}
    onkeydown={toggleFromKey}
  >
    {#if latest}
      {@const StatusIcon = iconFor(latest.status)}
      <span class="label">command</span>
      <span class="status status-{latest.status}" aria-label={latest.status}><StatusIcon size={13} /></span>
      <span class="command">
        {textFor(latest)}
      </span>
      <span class="meta">{latest.durationMs ? `${latest.durationMs} ms` : 'pending'}</span>
      <button
        type="button"
        class="icon-btn"
        aria-label="Copy latest history command"
        use:tooltip={{ content: 'Copy' }}
        onclick={copyLatest}
      ><Copy size={14} /></button>
    {:else}
      <span class="label">command</span>
      <span class="command empty">No actions yet</span>
    {/if}
    <span class="chev" aria-hidden="true">
      {#if history.expanded}<ChevronDown size={15} />{:else}<ChevronUp size={15} />{/if}
    </span>
  </div>
{/if}

<style>
  .history-bar {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 0;
    z-index: 1200;
    height: var(--history-bar-height);
    min-height: var(--history-bar-height);
    display: grid;
    grid-template-columns: auto auto minmax(0, 1fr) auto auto auto;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    padding: 0 var(--space-3);
    border: 0;
    border-top: 1px solid var(--border);
    border-radius: 0;
    background: rgba(13, 17, 23, 0.96);
    box-shadow: 0 -8px 24px rgba(0, 0, 0, 0.22);
    color: var(--dim);
    font-family: ui-monospace, monospace;
    font-size: var(--text-md);
    text-align: left;
  }
  .history-bar:hover {
    color: var(--fg);
    border-top-color: var(--dim);
  }
  .label {
    color: var(--blue);
    border: 1px solid rgba(88, 166, 255, 0.35);
    border-radius: var(--radius-sm);
    padding: 2px 6px;
    font-size: var(--text-xs);
    text-transform: uppercase;
  }
  .status, .command, .icon-btn, .chev {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    min-width: 0;
  }
  .status-ok { color: var(--green); }
  .status-error { color: var(--red); }
  .status-pending { color: var(--yellow); }
  .command {
    color: var(--fg);
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .command.empty { color: var(--dim); }
  .meta { color: var(--dim); white-space: nowrap; }
  button.icon-btn {
    justify-content: center;
    width: 24px;
    height: 22px;
    min-height: 22px;
    padding: 0;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--dim);
  }
  .icon-btn:hover { color: var(--fg); border-color: var(--dim); }
  .chev { justify-content: center; color: var(--dim); }
</style>
