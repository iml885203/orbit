<script lang="ts">
  import { push } from 'svelte-spa-router'
  import { fade, fly } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import { MediaQuery } from 'svelte/reactivity'
  import type { Trace, Span } from '$lib/types.gen'
  import { fetchTrace, fetchTraceLogs } from '$lib/tracing.svelte'
  import { playback } from '$lib/tracePlayback.svelte'
  import { subscribe } from '$lib/eventbus'
  import { formatDurationMs, selectSpanLogs, traceToText } from '$lib/traceTimeline'
  import { flashLogLine } from '$lib/logScroll'
  import { copyToClipboard } from '$lib/clipboard'
  import TraceWaterfall from './TraceWaterfall.svelte'
  import SpanInspector from './SpanInspector.svelte'
  import LogPanel from '../LogPanel.svelte'
  import LogModal from '../LogModal.svelte'

  // The trace detail as a routed dialog over the trace list. traceId comes
  // from the /tracing/:traceId route; onClose is history-aware (see the
  // parent TracingPage) so the URL and list state restore on close.
  let { traceId, onClose }: { traceId: string; onClose: () => void } = $props()

  let trace = $state<Trace | null>(null)
  let loading = $state(true)
  let gone = $state(false)
  let selectedSpanId = $state<string | null>(null)

  let logModalLines = $state<string[] | null>(null)
  let logModalLoading = $state(false)
  let copyOpen = $state(false)
  let copyEl = $state<HTMLDetailsElement>()

  // All log lines carrying this trace id, fetched once per trace. The synced
  // panel renders these; selecting a span scrolls to + flashes its lines.
  let allLogs = $state<string[]>([])
  let logHost = $state<HTMLDivElement | undefined>()

  const reducedMotionQuery = new MediaQuery('prefers-reduced-motion: reduce')
  const reducedMotion = $derived(reducedMotionQuery.current)
  let modal = $state<HTMLDivElement>()
  let returnFocus: HTMLElement | null = null

  // background=true is the live-update path (an SSE event for this trace):
  // refetch the spans quietly (no loading flicker) and skip the expensive
  // per-service log refetch — logs were loaded once on open.
  async function load(id: string, background = false) {
    if (!id) return
    if (!background) loading = true
    const { trace: t, gone: g } = await fetchTrace(id)
    trace = t
    gone = g
    loading = false
    // Default selection: the first errored span, else the root.
    if (t && selectedSpanId === null) {
      selectedSpanId = (t.spans.find((s) => s.status === 'error') ?? t.spans[0])?.spanId ?? null
    }
    if (t && !background) fetchTraceLogs(t.traceId).then((lines) => (allLogs = lines))
  }

  $effect(() => {
    selectedSpanId = null
    allLogs = []
    load(traceId)
  })

  // Focus management: capture the trigger (the trace row), move focus into the
  // dialog on open, restore it on close. Mirrors ConfirmModal.
  $effect(() => {
    returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    queueMicrotask(() => modal?.focus())
    return () => returnFocus?.focus()
  })

  // Lock background scroll while the dialog is mounted, so wheel events over
  // the backdrop don't scroll the list underneath.
  $effect(() => {
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => { document.body.style.overflow = prev }
  })

  // Integration: when the selected span changes, scroll the synced panel to
  // that span's first log line and flash it. Membership uses the exact span↔log
  // join (selectSpanLogs); no timestamp guessing.
  $effect(() => {
    const id = selectedSpanId
    if (!id || allLogs.length === 0 || !logHost) return
    const mine = selectSpanLogs(allLogs, id)
    if (mine.length === 0) return
    flashLogLine(logHost, allLogs.indexOf(mine[0]))
  })

  // Live: a trace accumulates spans over time, so refetch when an event for
  // this id arrives.
  $effect(() => {
    const id = traceId
    const unsub = subscribe('trace', (data) => {
      if ((data as { traceId: string }).traceId === id) load(id, true)
    })
    return unsub
  })

  const selectedSpan = $derived<Span | null>(
    trace?.spans.find((s) => s.spanId === selectedSpanId) ?? null,
  )

  function handleKey(e: KeyboardEvent) {
    // Defer to the nested "all logs" LogModal while it's open — it owns Esc
    // and its own scroll; a second Esc then closes this dialog.
    if (logModalLines !== null) return
    if (e.key === 'Escape') { e.preventDefault(); onClose(); return }
    if (e.key === 'Tab' && modal) {
      const controls = [...modal.querySelectorAll<HTMLElement>('button:not([disabled]), [href], input, [tabindex]:not([tabindex="-1"])')]
        .filter((el) => el.offsetParent !== null)
      if (controls.length === 0) return
      const first = controls[0]
      const last = controls[controls.length - 1]
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus() }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus() }
    }
  }

  function onWindowClick(e: MouseEvent) {
    if (copyOpen && copyEl && !copyEl.contains(e.target as Node)) copyOpen = false
  }

  async function openAllLogs() {
    if (!trace) return
    logModalLoading = true
    const lines = await fetchTraceLogs(trace.traceId)
    logModalLines = lines
    logModalLoading = false
  }

  // Copy actions. Trace is rendered as a pasteable text tree; logs are the
  // already-fetched trace-level lines; "both" stitches them for filing a bug
  // or handing to an agent.
  function copy(what: 'trace' | 'logs' | 'both') {
    if (!trace) return
    const traceText = traceToText(trace)
    const logsText = allLogs.join('\n')
    if (what === 'trace') {
      copyToClipboard(traceText, 'Copied trace')
    } else if (what === 'logs') {
      copyToClipboard(logsText || '(no logs for this trace)', `Copied ${allLogs.length} log line(s)`)
    } else {
      copyToClipboard(`${traceText}\n\n--- logs (${allLogs.length}) ---\n${logsText}`, 'Copied trace + logs')
    }
  }
</script>

<svelte:window onkeydown={handleKey} onclick={onWindowClick} />

<div class="backdrop" role="presentation" onclick={onClose}
  transition:fade={{ duration: reducedMotion ? 0 : 120 }}></div>
<div
  bind:this={modal}
  class="modal"
  role="dialog"
  aria-modal="true"
  aria-labelledby="trace-detail-title"
  tabindex="-1"
  transition:fly={{ y: reducedMotion ? 0 : 12, duration: reducedMotion ? 0 : 180, easing: cubicOut, opacity: reducedMotion ? 1 : 0 }}
>
  <div class="bar">
    <button type="button" class="back" onclick={onClose}>‹ Traces</button>
    <h2 id="trace-detail-title" class="root mono">
      {#if trace}{trace.rootService} {trace.rootName}{:else}Trace detail{/if}
    </h2>
    {#if trace}
      <span class="pill {trace.status === 'error' ? 'pill-err' : 'pill-ok'}" role="status">
        {trace.status === 'error' ? 'Error' : 'OK'} · {formatDurationMs(trace.durationMs)}
      </span>
      <span class="tid mono" title={trace.traceId}>{trace.traceId.slice(0, 12)}…</span>
      <span class="spacer"></span>
      <details class="copymenu" bind:open={copyOpen} bind:this={copyEl}>
        <summary class="action" aria-label="Copy trace or logs">⧉ Copy</summary>
        <div class="copypop" role="menu">
          <button type="button" role="menuitem" onclick={() => { copy('trace'); copyOpen = false }}>Copy trace</button>
          <button type="button" role="menuitem" onclick={() => { copy('logs'); copyOpen = false }}>Copy logs</button>
          <button type="button" role="menuitem" onclick={() => { copy('both'); copyOpen = false }}>Copy trace + logs</button>
        </div>
      </details>
      <button type="button" class="action play"
        onclick={() => { playback.start(trace!); push('/') }}>▶ Play on graph</button>
      <button type="button" class="action" disabled={logModalLoading} aria-busy={logModalLoading}
        onclick={openAllLogs}>{logModalLoading ? 'Loading…' : 'Open all logs ↗'}</button>
    {:else}
      <span class="spacer"></span>
    {/if}
    <button type="button" class="close" aria-label="Close trace detail" onclick={onClose}>✕</button>
  </div>

  {#if loading}
    <p class="msg" aria-busy="true">Loading trace…</p>
  {:else if gone}
    <div class="msg">
      <p>This trace has expired.</p>
      <p class="hint">It was dropped from the in-memory ring buffer. <span class="mono">{traceId}</span></p>
      <button type="button" class="link" onclick={onClose}>Back to traces</button>
    </div>
  {:else if !trace}
    <p class="msg">Trace not found.</p>
  {:else}
    <div class="split">
      <div class="left">
        <div class="wf">
          <div class="scale">
            <span>0</span><span>{formatDurationMs(trace.durationMs)}</span>
          </div>
          <TraceWaterfall {trace} {selectedSpanId} onSelect={(id) => (selectedSpanId = id)} />
        </div>
        <div class="trace-logs">
          <div class="tl-head">
            Trace logs <span class="tl-count">{allLogs.length} lines</span>
            <span class="tl-hint">selected span highlights here</span>
          </div>
          <div class="tl-body" bind:this={logHost}>
            {#if allLogs.length === 0}
              <p class="tl-empty">No log lines carry this trace id.</p>
            {:else}
              <LogPanel lines={allLogs} open id={'trace-logs-' + trace.traceId} maxHeight="220px" follow={false} actions />
            {/if}
          </div>
        </div>
      </div>
      <SpanInspector span={selectedSpan} />
    </div>
  {/if}
</div>

{#if logModalLines !== null && trace}
  <LogModal service={'trace ' + trace.traceId.slice(0, 8)} lines={logModalLines} onClose={() => (logModalLines = null)} />
{/if}

<style>
  .backdrop {
    position: fixed; inset: 0;
    background: rgba(13, 17, 23, 0.6);
    z-index: 900;
  }
  .modal {
    position: fixed;
    top: 50%; left: 50%;
    transform: translate(-50%, -50%);
    width: 95vw; max-width: 1400px; height: 90vh;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: 0 16px 48px rgba(0, 0, 0, 0.5);
    display: flex; flex-direction: column;
    min-height: 0; overflow: hidden;
    z-index: 901;
  }
  .bar {
    display: flex; align-items: center; gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border-bottom: 1px solid var(--border);
    background: var(--bg);
    flex-shrink: 0;
  }
  .mono { font-family: var(--font-mono); }
  .back, .action {
    background: color-mix(in srgb, var(--card) 50%, transparent);
    border: 1px solid var(--border); color: var(--fg);
    border-radius: var(--radius-md); padding: var(--space-1) var(--space-3);
    font-size: var(--text-md); cursor: pointer; font-family: inherit; min-height: var(--hit-target);
  }
  .back:hover, .action:hover { border-color: color-mix(in srgb, var(--blue) 42%, var(--border)); }
  .action.play { color: var(--blue); border-color: color-mix(in srgb, var(--blue) 40%, var(--border)); }
  .copymenu { position: relative; }
  .copymenu > summary { list-style: none; display: inline-flex; align-items: center; }
  .copymenu > summary::-webkit-details-marker { display: none; }
  .copypop {
    position: absolute; top: calc(100% + 4px); right: 0; z-index: 50;
    display: flex; flex-direction: column; min-width: 160px; padding: var(--space-1);
    background: var(--card); border: 1px solid var(--border);
    border-radius: var(--radius-md); box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
  }
  .copypop button {
    text-align: left; background: none; border: 0; color: var(--fg);
    padding: var(--space-2) var(--space-2); border-radius: var(--radius-sm);
    cursor: pointer; font-family: inherit; font-size: var(--text-md);
  }
  .copypop button:hover { background: color-mix(in srgb, var(--blue) 12%, transparent); }
  .root { font-weight: 600; font-size: var(--text-md); margin: 0; }
  .pill { border-radius: var(--radius-md); padding: 1px var(--space-2); font-size: var(--text-sm); }
  .pill-ok { color: var(--green); border: 1px solid color-mix(in srgb, var(--green) 40%, var(--border)); }
  .pill-err { color: var(--red); border: 1px solid color-mix(in srgb, var(--red) 40%, var(--border)); }
  .tid { color: var(--dim); font-size: var(--text-sm); }
  .spacer { flex: 1; }
  .close {
    background: none; border: 1px solid var(--border); color: var(--fg);
    border-radius: var(--radius-sm); cursor: pointer;
    width: var(--hit-target); height: var(--hit-target); min-width: var(--hit-target);
    display: inline-flex; align-items: center; justify-content: center;
  }
  .close:hover { background: var(--blue); color: white; }
  .split {
    display: grid;
    grid-template-columns: minmax(0, 1.7fr) minmax(280px, 0.8fr);
    flex: 1 1 auto; min-height: 0;
  }
  .left { display: flex; flex-direction: column; min-height: 0; overflow: hidden; }
  .wf { flex: 1 1 auto; overflow-y: auto; padding: 0 var(--space-3) var(--space-4); }
  .trace-logs { border-top: 1px solid var(--border); display: flex; flex-direction: column; flex-shrink: 0; }
  .tl-head {
    display: flex; align-items: center; gap: var(--space-2);
    padding: var(--space-1) var(--space-3); font-size: var(--text-sm); color: var(--fg);
    background: color-mix(in srgb, var(--card) 50%, transparent);
  }
  .tl-count { color: var(--dim); }
  .tl-hint { color: var(--dim); margin-left: auto; font-size: var(--text-xs); }
  .tl-body { min-height: 0; }
  .tl-empty { color: var(--dim); padding: var(--space-3); font-size: var(--text-md); }
  :global(.trace-logs .log-line.flash) { animation: tl-flash 0.6s ease-out; }
  @keyframes tl-flash {
    0% { outline: 2px solid var(--yellow); outline-offset: -2px; }
    100% { outline: 2px solid transparent; outline-offset: -2px; }
  }
  @media (prefers-reduced-motion: reduce) {
    :global(.trace-logs .log-line.flash) { animation: none; outline: 2px solid var(--yellow); outline-offset: -2px; }
  }
  .scale {
    display: flex; justify-content: space-between;
    color: var(--dim); font-size: var(--text-xs); font-family: var(--font-mono);
    padding: var(--space-2) var(--space-2) var(--space-1);
    position: sticky; top: 0; background: var(--bg);
    border-bottom: 1px solid var(--border);
  }
  .msg { padding: var(--space-6); color: var(--fg); text-align: center; }
  .msg .hint { color: var(--dim); font-size: var(--text-md); margin-top: var(--space-2); }
  .link { background: none; border: 0; color: var(--blue); cursor: pointer; font-size: var(--text-md); margin-top: var(--space-3); }
  .link:hover { text-decoration: underline; }
</style>
