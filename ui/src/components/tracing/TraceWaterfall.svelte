<script lang="ts">
  import type { Trace } from '$lib/types.gen'
  import { store } from '$lib/stores.svelte'
  import { kindColorVar, kindByName } from '$lib/kindColor'
  import { spanDepths, spanLayout, formatDurationMs } from '$lib/traceTimeline'

  let { trace, selectedSpanId, onSelect }: {
    trace: Trace
    selectedSpanId: string | null
    onSelect: (spanId: string) => void
  } = $props()

  const depths = $derived(spanDepths(trace.spans))
  // Resolve bar colour by service kind via a name→kind map built once per
  // render (errors are shown by the ✗ badge + outline, not by recolouring).
  const kinds = $derived(kindByName(store.graph.data?.nodes))

  // Virtualization: small traces render in full (zero risk, the common case);
  // large ones window to the visible range + a buffer so a 1000-span trace
  // stays responsive without a new UI library. Row height is fixed so the math
  // is exact. Spacer divs above/below preserve the scrollbar size.
  const ROW = 32
  const BUFFER = 10
  const VIRTUAL_THRESHOLD = 200
  const total = $derived(trace.spans.length)
  const virtual = $derived(total > VIRTUAL_THRESHOLD)

  let scroller = $state<HTMLDivElement>()
  let scrollTop = $state(0)
  let viewport = $state(600)

  const startIdx = $derived(virtual ? Math.max(0, Math.floor(scrollTop / ROW) - BUFFER) : 0)
  const endIdx = $derived(virtual ? Math.min(total, Math.ceil((scrollTop + viewport) / ROW) + BUFFER) : total)
  const visibleSpans = $derived(trace.spans.slice(startIdx, endIdx))
  const padTop = $derived(startIdx * ROW)
  const padBottom = $derived((total - endIdx) * ROW)

  function onScroll() {
    if (scroller) scrollTop = scroller.scrollTop
  }
</script>

<div
  class="waterfall"
  class:virtual
  role="group"
  aria-label="Span timeline"
  bind:this={scroller}
  bind:clientHeight={viewport}
  onscroll={virtual ? onScroll : undefined}
>
  {#if virtual && padTop > 0}<div style:height={`${padTop}px`} aria-hidden="true"></div>{/if}
  {#each visibleSpans as span (span.spanId)}
    {@const layout = spanLayout(span, trace.startUnixNano, trace.durationMs)}
    {@const err = span.status === 'error'}
    <button
      type="button"
      class="row"
      class:selected={span.spanId === selectedSpanId}
      class:err
      aria-pressed={span.spanId === selectedSpanId}
      aria-label={`${span.service} ${span.name} ${formatDurationMs(span.durationMs)}${err ? ' error' : ''}`}
      onclick={() => onSelect(span.spanId)}
    >
      <span class="label" style:padding-left={`${depths[span.spanId] * 14}px`}>
        <span class="svc">{span.service}</span>
        <span class="name">{span.name}</span>
      </span>
      <span class="track">
        <span
          class="bar"
          style:left={`${layout.offsetPct}%`}
          style:width={`${layout.widthPct}%`}
          style:background={kindColorVar(kinds[span.service])}
        ></span>
      </span>
      <span class="dur" class:err>{formatDurationMs(span.durationMs)}</span>
      <span class="pip" aria-hidden="true">{err ? '✗' : ''}</span>
    </button>
  {/each}
  {#if virtual && padBottom > 0}<div style:height={`${padBottom}px`} aria-hidden="true"></div>{/if}
</div>

<style>
  .waterfall {
    display: flex;
    flex-direction: column;
  }
  /* Virtual mode: the waterfall owns its scroll and rows are a fixed height so
     the windowing math is exact. Small traces never enter this branch. */
  .waterfall.virtual {
    overflow-y: auto;
    max-height: calc(100vh - 240px);
  }
  .waterfall.virtual .row {
    height: 32px;
    min-height: 32px;
  }
  .row {
    display: grid;
    grid-template-columns: minmax(160px, 240px) 1fr 64px 16px;
    gap: var(--space-2);
    align-items: center;
    min-height: 32px;
    padding: 0 var(--space-2);
    border: 0;
    border-bottom: 1px solid color-mix(in srgb, var(--border) 70%, transparent);
    background: transparent;
    color: var(--fg);
    font-family: inherit;
    text-align: left;
    cursor: pointer;
  }
  .row:hover { background: color-mix(in srgb, var(--blue) 6%, transparent); }
  .row.selected {
    background: color-mix(in srgb, var(--blue) 12%, transparent);
    box-shadow: inset 2px 0 0 var(--blue);
  }
  .row.err.selected { box-shadow: inset 2px 0 0 var(--red); }
  .label {
    display: flex;
    gap: var(--space-2);
    align-items: baseline;
    min-width: 0;
    font-size: var(--text-md);
  }
  .svc { font-family: var(--font-mono); color: var(--dim); flex-shrink: 0; }
  .name {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .track {
    position: relative;
    height: 10px;
    background: color-mix(in srgb, var(--fg) 7%, transparent);
    border-radius: var(--radius-sm);
  }
  .bar {
    position: absolute;
    top: 1px;
    height: 8px;
    min-width: 2px;
    border-radius: var(--radius-sm);
  }
  .row.err .bar {
    outline: 1px solid var(--red);
    outline-offset: 1px;
  }
  .dur {
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    color: var(--dim);
    text-align: right;
  }
  .dur.err { color: var(--red); }
  .pip { color: var(--red); font-size: var(--text-sm); text-align: center; }
</style>
