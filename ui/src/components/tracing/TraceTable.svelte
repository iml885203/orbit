<script lang="ts">
  import type { TraceSummary } from '$lib/types.gen'
  import { store } from '$lib/stores.svelte'
  import { kindColorVar, kindByName } from '$lib/kindColor'
  import { formatDurationMs, formatClock } from '$lib/traceTimeline'

  let { traces, onOpen }: {
    traces: TraceSummary[]
    onOpen: (traceId: string) => void
  } = $props()

  // Duration bars are a same-page relative comparison only — scaled to the
  // longest visible trace, not an absolute axis.
  const maxDur = $derived(Math.max(1, ...traces.map((t) => t.durationMs)))

  let rowEls: HTMLButtonElement[] = $state([])

  const kinds = $derived(kindByName(store.graph.data?.nodes))

  // Roving focus: Up/Down move between rows, Enter/Space open (native on a
  // <button>), so the whole list → detail path is keyboard-only.
  function onKey(e: KeyboardEvent, i: number) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      rowEls[Math.min(i + 1, traces.length - 1)]?.focus()
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      rowEls[Math.max(i - 1, 0)]?.focus()
    }
  }
</script>

<div class="table" role="table" aria-label="Traces">
  <div class="head" role="row">
    <span role="columnheader">Time</span>
    <span role="columnheader">Root</span>
    <span role="columnheader">Duration</span>
    <span role="columnheader">Services</span>
    <span role="columnheader">Status</span>
  </div>
  {#each traces as t, i (t.traceId)}
    <button
      type="button"
      class="row"
      class:err={t.status === 'error'}
      role="row"
      bind:this={rowEls[i]}
      onclick={() => onOpen(t.traceId)}
      onkeydown={(e) => onKey(e, i)}
    >
      <span class="time mono" role="cell">{formatClock(t.startUnixNano)}</span>
      <span class="root mono" role="cell">
        <span class="rsvc">{t.rootService}</span> {t.rootName}
      </span>
      <span class="dur" role="cell">
        <span class="bar-track">
          <span class="bar" class:err={t.status === 'error'} style:width={`${(t.durationMs / maxDur) * 100}%`}></span>
        </span>
        <span class="dur-val mono">{formatDurationMs(t.durationMs)}</span>
      </span>
      <span class="svcs" role="cell">
        {#each t.services as s (s)}
          <span class="dot" style:background={kindColorVar(kinds[s])} title={s}></span>
        {/each}
      </span>
      <span class="st" role="cell">
        {#if t.status === 'error'}
          <span class="pill pill-err" role="status">Error</span>
        {:else}
          <span class="pill pill-ok" role="status">OK</span>
        {/if}
      </span>
    </button>
  {/each}
</div>

<style>
  .table { display: flex; flex-direction: column; width: 100%; }
  .mono { font-family: var(--font-mono); }
  .head, .row {
    display: grid;
    grid-template-columns: 88px minmax(220px, 1.8fr) 1fr 120px 70px;
    gap: var(--space-3);
    align-items: center;
    padding: var(--space-2) var(--space-3);
  }
  .head {
    color: var(--dim);
    font-size: var(--text-xs);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    border-bottom: 1px solid var(--border);
  }
  .row {
    border: 0;
    border-bottom: 1px solid color-mix(in srgb, var(--border) 70%, transparent);
    background: transparent;
    color: var(--fg);
    font-size: var(--text-md);
    text-align: left;
    cursor: pointer;
    font-family: inherit;
  }
  .row:hover, .row:focus-visible { background: color-mix(in srgb, var(--blue) 7%, transparent); }
  .time { color: var(--dim); }
  .root { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .rsvc { color: var(--dim); }
  .dur { display: flex; align-items: center; gap: var(--space-2); }
  .bar-track {
    flex: 1;
    height: 6px;
    background: color-mix(in srgb, var(--fg) 8%, transparent);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .bar { display: block; height: 100%; background: var(--kind-backend); border-radius: var(--radius-sm); }
  .bar.err { background: var(--red); }
  .dur-val { font-size: var(--text-sm); color: var(--dim); flex-shrink: 0; }
  .svcs { display: flex; gap: 3px; flex-wrap: wrap; }
  .dot { width: 9px; height: 9px; border-radius: 2px; }
</style>
