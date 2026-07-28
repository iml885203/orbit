<script lang="ts">
  import { onMount } from 'svelte'
  import { CheckCircle2, Clock3, Copy, ScrollText, X, XCircle } from '@lucide/svelte'
  import { fetchHistoryList } from '$lib/api'
  import { history, type HistoryFilter, type HistoryRecord } from '$lib/history.svelte'
  import { openLogViewer, store, toast } from '$lib/stores.svelte'
  import { tooltip } from '$lib/tooltip.svelte'

  let { onclose }: { onclose: () => void } = $props()
  let onlyErrors = $state(false)
  let loading = $state(false)

  const filtered = $derived.by(() => {
    return history.records.filter((rec) => {
      if (onlyErrors && rec.status !== 'error') return false
      return true
    })
  })

  onMount(() => {
    topUp()
  })

  async function topUp() {
    loading = true
    const filter: HistoryFilter = { onlyErrors, limit: 200 }
    const records = await fetchHistoryList(filter)
    for (const rec of records.reverse()) history.upsert(rec)
    loading = false
  }

  async function copyRecord(rec: HistoryRecord) {
    const text = rec.command || rec.summary || ''
    if (!text) return
    await navigator.clipboard.writeText(text)
    toast('Copied command')
  }

  function setFilter(kind: 'all' | 'errors') {
    if (kind === 'all') {
      onlyErrors = false
    } else if (kind === 'errors') {
      onlyErrors = !onlyErrors
    }
    topUp()
  }

  // Best-effort service extraction from the recorded command so failed rows
  // get a one-click "open logs" escape hatch. Only offered when the token
  // matches a service the daemon actually knows — no guessing beyond that.
  function serviceOf(rec: HistoryRecord): string | null {
    const m = /^orbit (?:restart|stop|logs|up) ([a-z0-9-]+)/.exec(rec.command ?? '')
    if (m && store.daemon.services[m[1]]) return m[1]
    return null
  }

  function serviceWithLogs(rec: HistoryRecord): string | null {
    const service = serviceOf(rec)
    return service && store.daemon.services[service].logs_available ? service : null
  }

  function openLogs(name: string) {
    openLogViewer(name)
    onclose()
  }

  function iconFor(status: string) {
    if (status === 'ok') return CheckCircle2
    if (status === 'error') return XCircle
    return Clock3
  }
</script>

<section class="panel" aria-label="Command history">
  <div class="head">
    <span class="title">Command History</span>
    <button class:active={!onlyErrors} onclick={() => setFilter('all')}>All</button>
    <button class:active={onlyErrors} onclick={() => setFilter('errors')}>Errors</button>
    <span class="spacer"></span>
    <button class="icon" aria-label="Close command history" use:tooltip={{ content: 'Close' }} onclick={onclose}><X size={15} /></button>
  </div>

  <div class="rows">
    {#if loading && filtered.length === 0}
      <div class="empty">Loading commands…</div>
    {:else if filtered.length === 0}
      <div class="empty">No matching commands</div>
    {:else}
      {#each filtered as rec (rec.id)}
        {@const StatusIcon = iconFor(rec.status)}
        <div class="row">
          <span class="time">{new Date(rec.timestamp).toLocaleTimeString()}</span>
          <span class="status status-{rec.status}" aria-label={rec.status}><StatusIcon size={14} /></span>
          <span class="cmd">{rec.command || rec.summary}</span>
          <span class="duration">{rec.status === 'pending' ? 'pending' : `${rec.durationMs ?? 0} ms`}</span>
          {#if serviceWithLogs(rec)}
            <button class="copy" aria-label="Open logs for {serviceWithLogs(rec)}" use:tooltip={{ content: `Logs: ${serviceWithLogs(rec)}` }} onclick={() => openLogs(serviceWithLogs(rec)!)}><ScrollText size={14} /></button>
          {/if}
          <button class="copy" aria-label="Copy command" use:tooltip={{ content: 'Copy' }} onclick={() => copyRecord(rec)}><Copy size={14} /></button>
        </div>
        {#if rec.status === 'error' && rec.error}
          <div class="row-error" title={rec.error}>{rec.error}</div>
        {/if}
      {:else}
        <div class="empty">No matching commands</div>
      {/each}
    {/if}
  </div>
</section>

<style>
  .panel {
    position: fixed;
    left: 0;
    right: 0;
    bottom: var(--history-bar-height);
    z-index: 1199;
    height: min(42vh, 360px);
    display: grid;
    grid-template-rows: auto 1fr;
    background: rgba(13, 17, 23, 0.985);
    border-top: 1px solid var(--border);
    box-shadow: 0 -18px 38px rgba(0, 0, 0, 0.42);
    font-family: ui-monospace, monospace;
    font-size: var(--text-md);
  }
  .head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border-bottom: 1px solid var(--border);
  }
  .title { font-weight: 700; color: var(--fg); margin-right: var(--space-2); }
  .spacer { flex: 1; }
  button {
    min-height: 24px;
    padding: 3px 8px;
    border-radius: var(--radius-sm);
    font-size: var(--text-md);
    font-family: inherit;
  }
  button.active {
    color: var(--fg);
    border-color: var(--blue);
    background: rgba(88, 166, 255, 0.12);
  }
  button.icon, button.copy {
    width: 26px;
    padding: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .rows { overflow: auto; min-height: 0; }
  .row {
    display: grid;
    grid-template-columns: 86px 28px minmax(0, 1fr) 78px 32px;
    gap: var(--space-2);
    align-items: center;
    padding: var(--space-2) var(--space-3);
    border-bottom: 1px solid rgba(48, 54, 61, 0.72);
  }
  .row:hover { background: rgba(255, 255, 255, 0.035); }
  .time, .duration { color: var(--dim); }
  .status {
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .status-ok { color: var(--green); }
  .status-error { color: var(--red); }
  .status-pending { color: var(--yellow); }
  .cmd {
    min-width: 0;
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    color: var(--fg);
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .empty { padding: var(--space-5); color: var(--dim); }
  .row-error {
    padding: 0 var(--space-3) var(--space-1) 84px;
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    color: var(--red);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
