<script lang="ts">
  import { Handle, Position } from '@xyflow/svelte'
  import type { GraphNode } from '../../lib/types.gen'
  import { ICONS, COLORS } from '../../lib/constants'
  import { store, isRunning, openLogViewer, toast, mutationsDisabled } from '../../lib/stores.svelte'
  import { playback } from '../../lib/tracePlayback.svelte'
  import { apiPost } from '../../lib/api'
  import { Play, RotateCcw, Square, ExternalLink, AppWindow, ScrollText, Cog, Radio } from '@lucide/svelte'
  import { tooltip } from '../../lib/tooltip.svelte'
  import Icon from '@iconify/svelte'

  let { data: node, id }: { data: GraphNode; id: string } = $props()

  // Trace playback (additive; all false when no trace is playing). A node not
  // in the trace dims back; nodes on the revealed path light up; a failed
  // service pulses red.
  const pbOn = $derived(playback.active)
  const pbInTrace = $derived(pbOn && playback.inTrace(id))
  const pbActive = $derived(pbOn && playback.isServiceActive(id))
  const pbFailed = $derived(pbActive && playback.serviceFailed(id))

  const running = $derived(isRunning(node.state))
  const stopped = $derived(node.state === 'stopped' || node.state === 'pending')
  const infraIcon = $derived(node.kind === 'infra' ? node.icon : null)

  // First port entry for hint display
  const firstPort = $derived(
    node.ports ? Object.entries(node.ports)[0] : null
  )

  let busy = $state(false)
  // True when the active env is preview-only or hover-previewing another
  // env. Disables every mutation button below; daemon also rejects 409
  // but disabling avoids the round trip.
  const readOnly = $derived(mutationsDisabled())
  const showActions = $derived(!readOnly)

  // Show the infra icon strip only when the rendered graph is preview-only
  // (infra container nodes are hidden in that mode — the strip carries the
  // dependency info instead). For regular envs the strip is redundant with
  // the infra node + edge that the canvas already draws.
  const showInfraStrip = $derived(
    !!store.graph.active?.previewOnly && (node.infraDeps?.length ?? 0) > 0
  )

  // Kafka indicator: a small radio icon in row 1 that signals this node has
  // async (kafka) producers or consumers. The actual edges are revealed
  // only when the node is selected; the badge is the first-order hint that
  // there's anything to reveal at all. Tooltip carries topic names so users
  // can inspect the relationship without opening the drawer.
  const producesCount = $derived(node.kafka?.produces?.length ?? 0)
  const consumesCount = $derived(node.kafka?.consumes?.length ?? 0)
  const hasKafka = $derived(producesCount + consumesCount > 0)
  function formatTopicList(label: string, topics: string[] | undefined): string | null {
    if (!topics || topics.length === 0) return null
    return `${label} (${topics.length}): ${topics.join(', ')}`
  }
  const kafkaTooltip = $derived.by(() => {
    const lines = [
      formatTopicList('Produces', node.kafka?.produces),
      formatTopicList('Consumes', node.kafka?.consumes),
    ].filter((line): line is string => !!line)
    return lines.join('\n')
  })

  // Flash the state pill only on terminal transitions — entering or
  // leaving a stable state. Mid-progress states (starting / building)
  // already pulse the whole node, so flashing the pill again is noise.
  const TRANSIENT = new Set(['starting', 'building', 'stopping'])
  let flashing = $state(false)
  let prevState: string | undefined
  $effect(() => {
    const current = node.state
    if (
      prevState !== undefined &&
      prevState !== current &&
      !TRANSIENT.has(current) &&
      !TRANSIENT.has(prevState)
    ) {
      flashing = true
      const t = setTimeout(() => { flashing = false }, 700)
      prevState = current
      return () => clearTimeout(t)
    }
    prevState = current
  })

  function makeHandler(fn: () => void) {
    return (e: MouseEvent) => { e.stopPropagation(); fn() }
  }

  async function withBusy(fn: () => Promise<{ ok: boolean; data?: any }>, failMsg: string) {
    if (busy) return
    busy = true
    try {
      const { ok, data } = await fn()
      if (!ok) toast(data?.error || failMsg)
    } finally {
      busy = false
    }
  }

  async function doStart()   { await withBusy(() => apiPost('/api/up', { resources: [node.name] }), 'Failed to start') }
  async function doRestart() { await withBusy(() => apiPost('/api/restart/' + node.name),         'Failed to restart') }
  async function doStop()    { await withBusy(() => apiPost('/api/stop/' + node.name),            'Failed to stop') }

  function openLog(e: MouseEvent) {
    e.stopPropagation()
    openLogViewer(id)
  }

  function openUrl(e: MouseEvent) {
    e.stopPropagation()
    if (node.url) window.open(node.url, '_blank')
  }

  function onNodeKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      store.graph.selectedNode = node.name
    }
  }
</script>

<div
  class="node state-{node.state} kind-{node.kind}"
  class:pb-dim={pbOn && !pbInTrace}
  class:pb-active={pbActive}
  class:pb-failed={pbFailed}
  tabindex="0"
  role="button"
  aria-label="{node.name} — {node.state}{pbFailed ? ' — failed in trace' : ''}"
  onkeydown={onNodeKey}
>
  <Handle type="target" position={Position.Top} />

  <!-- Row 1: state dot + name + mode badge -->
  <div class="row row1">
    <span
      class="state-dot"
      class:flashing={flashing}
      style:color={COLORS[node.state]}
      aria-label={node.stateReason ? `${node.state} — ${node.stateReason}` : node.state}
      use:tooltip={{ content: node.stateReason ? `${node.state} — ${node.stateReason}` : node.state }}
    >{ICONS[node.state] ?? '?'}</span>
    {#if node.kind === 'infra'}
      <span class="infra-icon" data-testid="service-node-infra-icon" aria-hidden="true">
        {#if infraIcon}
          <Icon icon={infraIcon} width="16" height="16" />
        {:else}
          <Cog size={16} strokeWidth={2} />
        {/if}
      </span>
    {/if}
    <span class="name">{node.name}</span>
    {#if node.kind !== 'infra' && node.mode}
      <span class="mode-badge mode-{node.mode}">{node.mode}</span>
    {/if}
    {#if hasKafka}
      <span
        class="kafka-badge"
        aria-label={`Kafka — ${kafkaTooltip}`}
        use:tooltip={{ content: kafkaTooltip }}
      >
        <Radio size={11} strokeWidth={2.25} />
      </span>
    {/if}
  </div>

  <!-- Row 2: action buttons + port hint -->
  <div class="row row2">
    {#if showActions}
      <div class="actions">
        {#if stopped && !node.portConflict}
          <button
            class="action-btn"
            type="button"
            aria-label="Start {node.name}"
            use:tooltip={{ content: 'Start' }}
            disabled={busy}
            onclick={makeHandler(doStart)}
          ><Play size={15} strokeWidth={2} /></button>
        {/if}
        {#if running && !node.portConflict && !node.blockedBy}
          <button
            class="action-btn"
            type="button"
            aria-label="Restart {node.name}"
            use:tooltip={{ content: 'Restart' }}
            disabled={busy}
            onclick={makeHandler(doRestart)}
          ><RotateCcw size={15} strokeWidth={2} /></button>
          <button
            class="action-btn danger"
            type="button"
            aria-label="Stop {node.name}"
            use:tooltip={{ content: 'Stop' }}
            disabled={busy}
            onclick={makeHandler(doStop)}
          ><Square size={15} strokeWidth={2} fill="currentColor" /></button>
        {/if}
        {#if node.url}
          <button
            class="action-btn"
            type="button"
            aria-label="Open {node.name} in browser"
            use:tooltip={{ content: 'Open in browser' }}
            onclick={openUrl}
          ><ExternalLink size={15} strokeWidth={2} /></button>
        {/if}
        {#if node.sidecars}
          {#each node.sidecars as sc (sc.name)}
            <button
              class="action-btn"
              type="button"
              aria-label="Open {sc.name} UI"
              use:tooltip={{ content: sc.name }}
              onclick={makeHandler(() => window.open(sc.url, '_blank'))}
            ><AppWindow size={15} strokeWidth={2} /></button>
          {/each}
        {/if}
        {#if !node.portConflict || node.logsAvailable}
          <button
            class="action-btn"
            type="button"
            aria-label="Open logs for {node.name}"
            use:tooltip={{ content: 'Logs' }}
            onclick={openLog}
          ><ScrollText size={15} strokeWidth={2} /></button>
        {/if}
      </div>
    {/if}
    {#if showInfraStrip}
      <span class="infra-strip" aria-label="Infra dependencies for {node.name}">
        {#each node.infraDeps ?? [] as dep (dep.name)}
          <span class="infra-chip" use:tooltip={{ content: dep.name }} aria-label={dep.name}>
            {#if dep.icon}
              <Icon icon={dep.icon} width="14" height="14" />
            {:else}
              <Cog size={14} strokeWidth={2} />
            {/if}
          </span>
        {/each}
      </span>
    {/if}
    {#if firstPort}
      <span
        class="port-hint"
        class:clickable={!!node.url && showActions}
        role={node.url && showActions ? 'button' : undefined}
        onclick={node.url && showActions ? openUrl : undefined}
        use:tooltip={{ content: node.url && showActions ? 'Open in browser' : '' }}
      >:{firstPort[1]}</span>
    {/if}
  </div>

  <Handle type="source" position={Position.Bottom} />
</div>

<style>
  .node {
    width: 240px;
    min-height: 92px;
    border-radius: var(--radius-lg);
    border: 1px solid var(--border);
    background: var(--card);
    display: flex;
    flex-direction: column;
    justify-content: flex-start;
    /* gap would also apply to SvelteFlow's invisible Handle divs (top &
       bottom children of .node), inflating the node height. Use adjacent-
       sibling margin between .row elements instead so handles don't push
       the content apart. */
    padding: var(--space-2) var(--space-3);
    font-family: monospace;
    box-sizing: border-box;
  }
  .node.kind-frontend { background-color: color-mix(in srgb, var(--kind-frontend) 22%, var(--card)); }
  .node.kind-backend  { background-color: color-mix(in srgb, var(--kind-backend)  18%, var(--card)); }
  .node.kind-infra    { background-color: color-mix(in srgb, var(--kind-infra)    12%, var(--card)); }

  /* State-driven motion / opacity. Color encoding still lives on the pill;
     these effects add a secondary, glanceable signal you can read from
     across the canvas. */
  .node.state-stopped,
  .node.state-pending {
    opacity: 0.55;
    border-style: dashed;
  }
  .node.state-starting,
  .node.state-building {
    animation: node-breathe 1.6s ease-in-out infinite;
  }
  @keyframes node-breathe {
    0%, 100% { opacity: 1; }
    50%      { opacity: 0.55; }
  }
  @media (prefers-reduced-motion: reduce) {
    .node.state-starting,
    .node.state-building {
      animation: none;
    }
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .row1 {
    flex: 1 1 auto;
    min-height: 0;
  }

  .state-dot {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 14px;
    height: 14px;
    font-size: var(--text-md);
    line-height: 1;
    flex-shrink: 0;
    border-radius: 50%;
  }
  .state-dot.flashing {
    animation: dot-flash 700ms ease-out;
  }
  @keyframes dot-flash {
    0%   { box-shadow: 0 0 0 0 currentColor; }
    20%  { box-shadow: 0 0 0 5px color-mix(in srgb, currentColor 35%, transparent); }
    100% { box-shadow: 0 0 0 0 currentColor; }
  }
  @media (prefers-reduced-motion: reduce) {
    .state-dot.flashing { animation: none; }
  }

  .infra-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    color: var(--fg);
    flex-shrink: 0;
  }

  .name {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
    min-width: 0;
    font-size: var(--text-base);
  }

  .mode-badge {
    font-size: var(--text-xs);
    padding: 2px var(--space-2);
    border-radius: var(--radius-sm);
    background: var(--dim);
    color: var(--bg);
    flex-shrink: 0;
  }
  .mode-badge.mode-dev       { background: var(--blue); color: white; }
  .mode-badge.mode-container { background: var(--yellow); color: black; }

  /* Pushes the kafka badge to the far right of row1 so it lines up with
     the rightmost icon, mirroring how status sits on the left. */
  .kafka-badge {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    color: var(--blue);
    opacity: 0.75;
    flex-shrink: 0;
  }

  .row2 {
    justify-content: space-between;
    flex: 0 0 auto;
  }

  .actions {
    display: flex;
    align-items: center;
  }

  /* Infra dependency strip: shown only in preview-only envs where the
     infra container nodes themselves have been hidden. Visually quieter
     than the action buttons — these are read-only affordances. Sits to
     the right of the actions, before the port hint. */
  .infra-strip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    margin-left: auto;
    padding-left: var(--space-2);
    border-left: 1px solid var(--border);
  }
  .infra-chip {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    color: var(--dim);
    opacity: 0.7;
  }

  .action-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: 1px solid transparent;
    color: var(--dim);
    cursor: pointer;
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-sm);
    line-height: 0;
    min-width: var(--hit-target);
    min-height: var(--hit-target);
    transition: color var(--transition-fast), border-color var(--transition-fast), background var(--transition-fast), transform var(--transition-press);
  }
  .action-btn:active:not(:disabled) {
    transform: scale(0.92);
  }
  .action-btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .action-btn:hover {
    color: var(--fg);
    border-color: var(--border, var(--dim));
    background: rgba(255, 255, 255, 0.07);
  }
  .action-btn.danger:hover {
    color: var(--red);
    border-color: var(--red);
  }

  .port-hint {
    font-size: var(--text-sm);
    color: var(--fg);
    opacity: 0.65;
    flex-shrink: 0;
  }
  .port-hint.clickable {
    cursor: pointer;
  }
  .port-hint.clickable:hover {
    color: var(--blue);
    opacity: 1;
  }

  /* Trace playback: nodes outside the trace recede; nodes on the revealed
     path lift with an accent ring; a failed service pulses red. State/kind
     colours are untouched — playback only adds dim/lift/pulse. */
  .node.pb-dim {
    opacity: 0.32;
    filter: saturate(0.5);
  }
  .node.pb-active {
    box-shadow:
      0 0 0 1px color-mix(in srgb, var(--blue) 60%, transparent),
      0 0 16px color-mix(in srgb, var(--blue) 30%, transparent);
  }
  .node.pb-failed {
    box-shadow:
      0 0 0 1px var(--red),
      0 0 18px color-mix(in srgb, var(--red) 38%, transparent);
    animation: pb-pulse 1.4s ease-in-out infinite;
  }
  @keyframes pb-pulse {
    0%, 100% { box-shadow: 0 0 0 1px var(--red), 0 0 10px color-mix(in srgb, var(--red) 30%, transparent); }
    50%      { box-shadow: 0 0 0 1px var(--red), 0 0 22px color-mix(in srgb, var(--red) 55%, transparent); }
  }
  @media (prefers-reduced-motion: reduce) {
    /* Static red ring, no pulse — the step badge + outline still convey it. */
    .node.pb-failed { animation: none; }
  }
</style>
