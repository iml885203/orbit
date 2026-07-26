<script lang="ts">
  import { analyzeLines, isNarration } from '$lib/logClassify'
  import { extractTraceId, traceColor } from '$lib/traceColor'
  import { copyToClipboard } from '$lib/clipboard'

  let { lines, open, id, maxHeight = '300px', follow = true, actions = false, onOpenTrace }: {
    lines: string[]
    open: boolean
    id: string
    maxHeight?: string
    follow?: boolean
    // When true, render per-line copy actions (📋 / 🧵). Off by default
    // so the inline panel stays clean — only the modal opts in.
    actions?: boolean
    // Integration ①: hosts that can navigate to a trace pass this; the 🔍
    // button only renders when set. Routing stays with the host — the log
    // domain doesn't know about the tracing routes.
    onOpenTrace?: (traceId: string) => void
  } = $props()

  let el: HTMLDivElement | undefined = $state()

  // One pass across lines: classify level/source AND track error-group head.
  const meta = $derived(analyzeLines(lines))
  // Per-line raw trace IDs (from the line text itself).
  const lineTraces = $derived(lines.map(extractTraceId))
  // Per-line effective trace: stack frames inherit the head's trace
  // because they themselves contain no TraceId.
  const effectiveTraces = $derived(
    lineTraces.map((t, i) => {
      const head = meta[i].head
      if (head !== -1 && lineTraces[head]) return lineTraces[head]
      return t
    })
  )

  $effect(() => {
    if (open && follow && lines.length && el) {
      el.scrollTop = el.scrollHeight
    }
  })

  function copyLineOrGroup(idx: number) {
    const head = meta[idx].head
    if (head !== -1) {
      const groupLines = lines.filter((_, i) => meta[i].head === head)
      copyToClipboard(groupLines.join('\n'), `Copied error (${groupLines.length} lines)`)
    } else {
      copyToClipboard(lines[idx], 'Line copied')
    }
  }

  function copyTraceFor(idx: number) {
    const trace = effectiveTraces[idx]
    if (!trace) return
    const matched = lines.filter((_, i) => effectiveTraces[i] === trace).join('\n')
    copyToClipboard(matched, `Copied trace (${matched.split('\n').length} lines)`)
  }

  // Integration ①: jump from a log line's trace to the full trace waterfall.
  // The log's TraceId is the same W3C id the OTLP receiver keys on, so the
  // detail page resolves it directly (or shows "expired" if evicted).
  function openTrace(idx: number) {
    const trace = effectiveTraces[idx]
    if (trace && onOpenTrace) onOpenTrace(trace)
  }
</script>

<div class="logs-panel" class:open style="--max-h: {maxHeight}">
  <div class="logs-inner" bind:this={el} {id}>
    {#each lines as line, i (i)}
      {@const m = meta[i]}
      {@const trace = effectiveTraces[i]}
      <!-- Continuation lines inherit 'error' from their head; m.head is
           only set when the head was an error-level line. -->
      {@const level = m.head !== -1 ? 'error' : m.level}
      <div
        class="log-line lvl-{level} src-{m.source}"
        class:src-narration={isNarration(m.source)}
        data-line-index={i}
        style:--trace-color={trace ? traceColor(trace) : 'transparent'}
      >
        <span class="line-text">{line}</span>
        {#if actions}
          <span class="line-actions">
            <button
              type="button"
              class="line-btn"
              title={m.head !== -1 ? 'Copy this error (head + stack)' : 'Copy this line'}
              aria-label="Copy"
              onclick={() => copyLineOrGroup(i)}
            >📋</button>
            {#if trace}
              <button
                type="button"
                class="line-btn"
                title="Copy all lines for this trace"
                aria-label="Copy trace"
                onclick={() => copyTraceFor(i)}
              >🧵</button>
              {#if onOpenTrace}
                <button
                  type="button"
                  class="line-btn"
                  title="Open this trace in the waterfall"
                  aria-label="Open trace"
                  onclick={() => openTrace(i)}
                >🔍</button>
              {/if}
            {/if}
          </span>
        {/if}
      </div>
    {/each}
  </div>
</div>

<style>
  .logs-panel {
    position: relative;
    max-height: 0;
    overflow: hidden;
    transition: max-height 0.3s ease;
  }
  .logs-panel.open {
    max-height: var(--max-h);
  }
  .logs-inner {
    height: var(--max-h);
    overflow-y: auto;
    padding: var(--space-2);
    font-family: 'SF Mono', Monaco, monospace;
    font-size: var(--text-md);
    line-height: 1.5;
    background: var(--bg);
  }
  /* Each line: trace color is a low-opacity background tint covering
     the whole row (so consecutive same-trace lines visually group as
     a block), while the error/warn level bar stays as a single 3px
     left stripe — the most signal for the smallest visual cost. */
  /* Left edge = trace identity (which request this line belongs to).
     Row background = level signal (info / warn / error). The two carry
     orthogonal information and don't compete visually. */
  .log-line {
    position: relative;
    padding: 0 var(--space-1);
    border-left: 4px solid var(--trace-color, transparent);
    white-space: pre-wrap;
    word-break: break-word;
    color: var(--fg);
    display: flex;
    align-items: flex-start;
    gap: var(--space-1);
  }
  .line-text {
    flex: 1;
  }
  /* Narration lines (orbit lifecycle, init, settings, pre_start, etc.)
     are muted so the eye skips them. Order matters: this rule comes
     before .lvl-* so error/warn override mute when both classes apply
     (e.g., "[poller] error: ..."). The single .src-narration class is
     toggled by isNarration() so new sources don't require CSS edits. */
  .log-line.src-narration {
    color: var(--dim);
  }
  .log-line.lvl-error {
    background-color: color-mix(in srgb, var(--red) 10%, transparent);
    color: var(--fg);
  }
  .log-line.lvl-warn {
    background-color: color-mix(in srgb, var(--yellow) 10%, transparent);
    color: var(--fg);
  }
  /* Hover highlight: muted source lines pop to full foreground, and
     every row gets a faint white tint so it's clear which one your
     cursor is on. The tint stacks over any existing trace / level
     background via background-image. */
  .log-line:hover {
    color: var(--fg);
    background-image: linear-gradient(rgba(255, 255, 255, 0.05), rgba(255, 255, 255, 0.05));
  }
  .line-actions {
    flex-shrink: 0;
    display: flex;
    gap: 2px; /* 1px/2px border exception */
    opacity: 0;
    transition: opacity var(--transition-fast);
  }
  .log-line:hover .line-actions {
    opacity: 1;
  }
  .line-btn {
    background: rgba(0, 0, 0, 0.4);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 3px; /* off-grid: between 0 and radius-sm */
    box-sizing: border-box;
    width: 22px;
    height: 22px;
    min-width: 22px;
    min-height: 22px;
    font-size: var(--text-md);
    line-height: 1;
    cursor: pointer;
    padding: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    overflow: hidden;
  }
  .line-btn:hover {
    background: var(--blue);
    color: var(--white);
  }
</style>
