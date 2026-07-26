<script lang="ts">
  import LogPanel from './LogPanel.svelte'
  import Btn from './Btn.svelte'
  import { analyzeLines } from '$lib/logClassify'
  import { flashLogLine } from '$lib/logScroll'

  let { service, lines, onClose, onOpenTrace }: {
    service: string
    lines: string[]
    onClose: () => void
    // Forwarded to LogPanel — hosts that can navigate to a trace pass it.
    onOpenTrace?: (traceId: string) => void
  } = $props()

  let follow = $state(true)
  let panelEl: HTMLDivElement | undefined = $state()
  let selectedErrorGroupIndex = $state<number | null>(null)
  let filterText = $state('')
  let filterLevel = $state<'all' | 'error' | 'warn'>('all')

  // Filtering happens here (not in LogPanel) so the panel stays a dumb
  // renderer. Level uses the head-aware effective level — a stack frame
  // belongs to its error head — and text is a case-insensitive substring.
  const filteredLines = $derived.by(() => {
    if (!filterText.trim() && filterLevel === 'all') return lines
    const meta = analyzeLines(lines)
    const q = filterText.trim().toLowerCase()
    return lines.filter((line, i) => {
      const eff = meta[i].head !== -1 ? 'error' : meta[i].level
      if (filterLevel !== 'all' && eff !== filterLevel) return false
      if (q && !line.toLowerCase().includes(q)) return false
      return true
    })
  })

  // Each entry is the line index of an error-group head (the [ERR] line).
  // Indented stack frames following it are absorbed into that group.
  // analyzeLines walks the lines once to get level+source+head; we filter
  // for the head positions instead of running a second classification pass.
  // Computed over the FILTERED view so prev/next error navigates what is
  // actually visible.
  const errorGroups = $derived(
    analyzeLines(filteredLines).reduce<number[]>((acc, m, i) => {
      if (m.head === i) acc.push(i)
      return acc
    }, [])
  )

  function jumpToLine(index: number) {
    // turning follow off: the user just jumped to a specific error,
    // they don't want auto-scroll yanking them away.
    follow = false
    flashLogLine(panelEl, index)
  }

  function jumpRelativeError(direction: 1 | -1) {
    const n = errorGroups.length
    if (n === 0) return
    if (selectedErrorGroupIndex === null || selectedErrorGroupIndex >= n) {
      // Either first press, or the buffer trimmed past our cursor —
      // restart from the appropriate end.
      selectedErrorGroupIndex = direction === 1 ? 0 : n - 1
    } else {
      selectedErrorGroupIndex = (selectedErrorGroupIndex + direction + n) % n
    }
    jumpToLine(errorGroups[selectedErrorGroupIndex])
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      onClose()
    } else if (e.key === 'j' || e.key === 'ArrowDown') {
      e.preventDefault()
      jumpRelativeError(1)
    } else if (e.key === 'k' || e.key === 'ArrowUp') {
      e.preventDefault()
      jumpRelativeError(-1)
    }
  }

  // Lock background scroll while the modal is mounted. Without this,
  // wheel events outside the modal scroll the dashboard underneath.
  $effect(() => {
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => { document.body.style.overflow = prev }
  })

  $effect(() => {
    const inner = panelEl?.querySelector('.logs-inner') as HTMLElement | null
    if (!inner) return
    function onScroll() {
      const atBottom = inner!.scrollHeight - inner!.scrollTop - inner!.clientHeight < 8
      // when the user scrolls back to the bottom, snap fully to the latest line
      // so re-enabling follow is visually immediate (next stream line could be seconds away).
      if (atBottom && !follow) {
        inner!.scrollTop = inner!.scrollHeight
      }
      follow = atBottom
    }
    inner.addEventListener('scroll', onScroll)
    return () => inner.removeEventListener('scroll', onScroll)
  })
</script>

<svelte:window onkeydown={onKey} />

<!-- Backdrop is click-to-close only. Esc and the Close button cover keyboard a11y. -->
<div class="backdrop" onclick={onClose} role="presentation">
  <div
    class="modal"
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => e.stopPropagation()}
    role="dialog"
    aria-modal="true"
    aria-label="Log viewer for {service}"
    tabindex="-1"
  >
    <header>
      <span class="title">{service}</span>
      <input
        class="filter-input"
        type="search"
        placeholder="Filter…"
        bind:value={filterText}
        aria-label="Filter log lines"
      />
      <div class="level-filter" role="group" aria-label="Level filter">
        <button type="button" class:active={filterLevel === 'all'} aria-pressed={filterLevel === 'all'} onclick={() => (filterLevel = 'all')}>All</button>
        <button type="button" class:active={filterLevel === 'error'} aria-pressed={filterLevel === 'error'} onclick={() => (filterLevel = 'error')}>Err</button>
        <button type="button" class:active={filterLevel === 'warn'} aria-pressed={filterLevel === 'warn'} onclick={() => (filterLevel = 'warn')}>Warn</button>
      </div>
      <span class="meta">
        {filteredLines.length === lines.length ? `${lines.length} lines` : `${filteredLines.length}/${lines.length} lines`} ·
        {#if errorGroups.length === 0}
          0 errors
        {:else if selectedErrorGroupIndex === null}
          {errorGroups.length} errors
        {:else}
          error {selectedErrorGroupIndex + 1} / {errorGroups.length}
        {/if}
      </span>
      <Btn label="⏮ prev err" disabled={errorGroups.length === 0} onclick={() => jumpRelativeError(-1)} />
      <Btn label="⏭ next err" disabled={errorGroups.length === 0} onclick={() => jumpRelativeError(1)} />
      <label class="follow">
        <input type="checkbox" bind:checked={follow} />
        Follow
      </label>
      <button type="button" class="close" onclick={onClose} aria-label="Close">✕</button>
    </header>
    <div class="body" bind:this={panelEl}>
      <LogPanel
        lines={filteredLines}
        open
        id={'modal-logs-' + service}
        maxHeight="calc(90vh - 3rem)"
        {follow}
        actions
        {onOpenTrace}
      />
    </div>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.7);
    z-index: 1000;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .modal {
    width: 95vw;
    max-width: 1400px;
    height: 90vh;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  header {
    display: flex;
    align-items: center;
    gap: 0.6rem; /* off-grid: intentional compact header rhythm */
    padding: var(--space-2) var(--space-2);
    border-bottom: 1px solid var(--border);
    background: var(--bg);
  }
  .title {
    font-weight: 600;
  }
  .meta {
    color: var(--dim);
    font-size: var(--text-md);
    margin-right: auto;
  }
  .close {
    background: none;
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-size: var(--text-md);
    box-sizing: border-box;
    width: 28px;
    height: 28px;
    min-width: 28px;
    min-height: 28px;
    padding: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    overflow: hidden;
  }
  .close:hover {
    background: var(--blue);
    color: var(--white);
  }
  .filter-input {
    background: var(--card);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: var(--radius-sm);
    padding: 2px 8px;
    font-size: var(--text-md);
    width: 180px;
    min-height: 28px;
  }
  .level-filter {
    display: inline-flex;
    gap: 2px;
    padding: 2px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }
  .level-filter button {
    background: none;
    border: 0;
    color: var(--dim);
    font-size: var(--text-sm);
    padding: 2px 8px;
    border-radius: 3px;
    cursor: pointer;
    font-family: inherit;
  }
  .level-filter button.active {
    background: color-mix(in srgb, var(--blue) 18%, transparent);
    color: var(--fg);
  }
  .follow {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    font-size: var(--text-md);
    color: var(--dim);
  }
  .body {
    flex: 1;
    min-height: 0;
    overflow: hidden;
  }
  /* Flash animation for the jump-to-error highlight (lives here so the
     modal owns it; LogPanel doesn't know about it). */
  :global(.log-line.flash) {
    animation: flash-pulse 0.6s ease-out;
  }
  @keyframes flash-pulse {
    0% { outline: 2px solid var(--yellow); outline-offset: -2px; }
    100% { outline: 2px solid transparent; outline-offset: -2px; }
  }
</style>
