<script lang="ts">
  import { BaseEdge, EdgeLabel, type EdgeProps } from '@xyflow/svelte'
  import { store } from '../../lib/stores.svelte'
  import { playback } from '../../lib/tracePlayback.svelte'
  import { liveTraffic } from '../../lib/liveTraffic.svelte'
  import type { RoutedGraphEdge } from './edge-routing'
  import { buildDependencyPath } from './edge-path'
  import { stableHash } from '../../lib/hash'

  let props: EdgeProps = $props()
  const data = $derived(props.data as (RoutedGraphEdge & Record<string, unknown>) | undefined)
  const isAsync = $derived(data?.kind === 'async')
  const topic = $derived(data?.topic ?? '')
  const detached = $derived(!!data?.detached)

  // Edge is "live" when both endpoints are healthy — dependency traffic is
  // actually flowing. We draw one travelling dot from source to target.
  // Async edges never light up — traffic is fire-and-forget through a broker.
  const live = $derived.by(() => {
    if (isAsync) return false
    if (detached) return false
    const g = store.graph.data
    if (!g) return false
    const from = g.nodes.find(n => n.name === props.source)
    const to   = g.nodes.find(n => n.name === props.target)
    return !!(from && to && from.state === 'healthy' && to.state === 'healthy')
  })

  const pathData = $derived(buildDependencyPath({
    id: props.id,
    async: isAsync,
    sourceX: props.sourceX, sourceY: props.sourceY,
    sourcePosition: props.sourcePosition,
    targetX: props.targetX, targetY: props.targetY,
    targetPosition: props.targetPosition,
    routeLane: data?.routeLane ?? 0,
  }))
  const path = $derived(pathData.path)
  const labelX = $derived(pathData.labelX)
  const labelY = $derived(pathData.labelY)
  // Per-edge stable hash → small offset that spreads labels when many
  // async edges still converge on the same midpoint (e.g. multiple
  // producers feeding settlement-worker — same target node, similar curvature).
  // Hash to keep the offset stable across renders.
  const labelOffset = $derived.by(() => {
    if (!isAsync) return { x: 0, y: 0 }
    const h = stableHash(props.id)
    // 5-step grid: -20, -10, 0, 10, 20px in each axis.
    const xs = ((h % 5) - 2) * 10
    const ys = (((h >> 8) % 5) - 2) * 10
    return { x: xs, y: ys }
  })

  // Stagger the start of each edge's flow dot so the whole graph
  // doesn't pulse in lockstep. Hash the edge id into a stable
  // 0..LOOP-second offset — same edge always gets the same delay,
  // different edges get different ones.
  const LOOP = 3
  const beginOffset = $derived.by(() => (stableHash(props.id) % 1000) / 1000 * LOOP)

  function onClick(e: MouseEvent) {
    store.graph.selectedEdge = { from: props.source, to: props.target, x: e.clientX, y: e.clientY }
  }

  // Trace playback styling (additive; baseStyle is the normal look). On the
  // revealed path the hop glows (red for the failing hop); off-path edges
  // recede.
  const baseStyle = $derived(
    isAsync ? 'stroke: var(--blue, #3b82f6); stroke-dasharray: 6 4; stroke-opacity: 0.7;' : '',
  )
  const pbActive = $derived(playback.active && playback.isEdgeActive(props.source, props.target))
  // Ambient live-traffic pulse (liveTraffic domain, not playback): only when
  // no specific trace is being replayed.
  const ambientLive = $derived(!playback.active && !isAsync && liveTraffic.isEdgeLive(props.source, props.target))
  const pbError = $derived(pbActive && playback.serviceFailed(props.target))
  const edgeStyle = $derived(
    !playback.active
      ? baseStyle
      : pbActive
        ? (pbError
            ? 'stroke: var(--red); stroke-width: 2.5; stroke-opacity: 1; stroke-dasharray: none;'
            : 'stroke: var(--blue); stroke-width: 2.5; stroke-opacity: 1; stroke-dasharray: none;')
        : 'stroke-opacity: 0.12;',
  )

  function onHitKey(e: KeyboardEvent) {
    if (e.key !== 'Enter' && e.key !== ' ') return
    e.preventDefault()
    const target = e.currentTarget as SVGPathElement
    const rect = target.getBoundingClientRect()
    store.graph.selectedEdge = {
      from: props.source,
      to: props.target,
      x: rect.left + rect.width / 2,
      y: rect.top + rect.height / 2,
    }
  }
</script>

<BaseEdge
  id={props.id}
  path={path}
  class={detached ? 'dep-edge dep-edge-detached' : 'dep-edge'}
  style={edgeStyle}
  interactionWidth={20}
/>

{#if isAsync && topic}
  <EdgeLabel
    x={labelX + labelOffset.x}
    y={labelY + labelOffset.y}
    class="topic-label"
    transparent
  >
    {topic}
  </EdgeLabel>
{/if}

<!-- Travelling dot — one dot per edge, moves source → target then waits.
     keyTimes splits the 3s loop into 1.5s of motion + 1.5s of rest;
     opacity animation fades the dot out at the end of the motion so the
     wait doesn't show a frozen dot at the target. -->
{#if live || pbActive || ambientLive}
  <circle r="3" fill={pbError ? 'var(--red)' : 'var(--white)'} class="flow-dot" opacity="0">
    <animateMotion
      dur="3s"
      begin="-{beginOffset}s"
      repeatCount="indefinite"
      path={path}
      rotate="auto"
      keyPoints="0; 1; 1"
      keyTimes="0; 0.5; 1"
      calcMode="linear"
    />
    <animate
      attributeName="opacity"
      dur="3s"
      begin="-{beginOffset}s"
      repeatCount="indefinite"
      values="0; 1; 1; 0; 0"
      keyTimes="0; 0.08; 0.45; 0.55; 1"
    />
  </circle>
{/if}

<!-- transparent wider click hit -->
<path
  d={path}
  fill="none"
  stroke="transparent"
  stroke-width="16"
  style="cursor: pointer;"
  onclick={onClick}
  onkeydown={onHitKey}
  role="button"
  aria-label="dependency from {props.source} to {props.target}"
  tabindex="0"
/>

<style>
  :global(.dep-edge) {
    stroke: var(--dim);
    stroke-width: 1.5;
    transition: stroke 0.3s, opacity 0.3s, stroke-dasharray 0.3s;
  }
  :global(.dep-edge-detached) {
    stroke: var(--dim);
    stroke-dasharray: 6 4;
    opacity: 0.5;
  }
  .flow-dot {
    filter: drop-shadow(0 0 3px rgba(255, 255, 255, 0.6));
  }
  :global(.topic-label) {
    pointer-events: none;
    background: color-mix(in srgb, var(--card) 90%, transparent);
    color: var(--blue, #3b82f6);
    font-size: 10px;
    padding: 2px 6px;
    border-radius: 3px;
    font-family: monospace;
    white-space: nowrap;
  }
  @media (prefers-reduced-motion: reduce) {
    .flow-dot { display: none; }
  }
</style>
