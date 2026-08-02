<script lang="ts">
  // GroupNode is a non-interactive container that visually groups
  // the services belonging to one group (defined in envs/*.yaml under
  // `groups.<name>.services`). Position + size is computed by layout.ts;
  // this component only owns the visual chrome (label + border + tint).
  //
  // Tint resolution:
  //   1. Use data.color verbatim if provided (yaml `groups.<name>.color`).
  //   2. Otherwise derive a stable hue from the group name (string hash).
  // Either way the same value drives both border and background via two
  // CSS custom properties exposed inline.
  import { hashHue } from '../../lib/hash'
  import { store, toast, mutationsDisabled, isRunning } from '../../lib/stores.svelte'
  import { apiPost } from '../../lib/api'
  import { Play, Square } from '@lucide/svelte'
  import { tooltip } from '../../lib/tooltip.svelte'

  type Props = { data: { name: string; color?: string; serviceCount: number } }
  let { data }: Props = $props()

  // Same group name always lands on the same hue across reloads.
  // Saturation/lightness fixed so auto colors land in the same muted
  // palette as the deliberately picked ones.
  const tint = $derived(data.color ?? `hsl(${hashHue(data.name)} 60% 55%)`)

  // Services declared in this group (for the per-group stop fan-out).
  const services = $derived(
    store.graph.active?.groups?.find(g => g.name === data.name)?.services ?? [],
  )
  const groupNodes = $derived(
    (store.graph.active?.nodes ?? []).filter(node => services.includes(node.name)),
  )
  const groupChanging = $derived(
    groupNodes.some(node => ['starting', 'building', 'stopping', 'restarting'].includes(node.state)),
  )
  const canStart = $derived(
    !groupChanging && groupNodes.some(node => node.state === 'stopped' || node.state === 'pending'),
  )
  const canStop = $derived(!groupChanging && groupNodes.some(node => isRunning(node.state)))
  // Another env's on-disk preview has no live processes to mutate.
  const showActions = $derived(!mutationsDisabled() && services.length > 0)

  let busy = $state(false)

  // Start the whole group: /api/up resolves the group's services *and* their
  // dependencies (e.g. starting "game" also brings up billing/settlement-worker).
  async function doStart() {
    if (busy) return
    busy = true
    try {
      const { ok, data: resp } = await apiPost('/api/up', { groups: [data.name] })
      if (!ok) toast(resp?.error || `Failed to start ${data.name}`)
    } finally {
      busy = false
    }
  }

  // Shared dependencies stay alive because another group may still need them.
  async function doStop() {
    if (busy) return
    busy = true
    try {
      const { ok, data: resp } = await apiPost('/api/down', { groups: [data.name] })
      if (!ok) toast(resp?.error || `Failed to stop ${data.name}`)
    } finally {
      busy = false
    }
  }

  function onStart(e: MouseEvent) { e.stopPropagation(); doStart() }
  function onStop(e: MouseEvent) { e.stopPropagation(); doStop() }
</script>

<div
  class="feature-group"
  style:--group-tint={tint}
>
  <span class="label">
    {data.name}<span class="count">{data.serviceCount}</span>
    {#if showActions}
      <span class="group-actions">
        <button
          class="gbtn"
          disabled={busy || !canStart}
          aria-busy={busy || groupChanging}
          aria-label="Start {data.name} group"
          use:tooltip={{ content: 'Start group' }}
          onclick={onStart}
        >
          <Play size={12} />
        </button>
        <button
          class="gbtn"
          disabled={busy || !canStop}
          aria-busy={busy || groupChanging}
          aria-label="Stop {data.name} group"
          use:tooltip={{ content: 'Stop group' }}
          onclick={onStop}
        >
          <Square size={12} />
        </button>
      </span>
    {/if}
  </span>
</div>

<style>
  /* SvelteFlow wraps every group node in a .svelte-flow__node-group div
     with a default panel background (rgba(240,240,240,0.25)) and border.
     On our dark canvas that reads as a stray white layer. :global() lets
     us strip the wrapper's chrome so only our .feature-group below paints. */
  :global(.svelte-flow__node-group) {
    background: transparent;
    border: none;
    box-shadow: none;
  }

  .feature-group {
    width: 100%;
    height: 100%;
    border-radius: var(--radius-lg);
    border: 1px dashed color-mix(in srgb, var(--group-tint) 55%, var(--border));
    /* No fill — a translucent tint over a dark canvas reads as a brighter
       "second layer" behind the service nodes, which is visually noisy.
       Border + label tint alone carries the grouping signal. */
    background: transparent;
    box-sizing: border-box;
    position: relative;
    /* Group is a layout-only artifact; clicks pass through to children. */
    pointer-events: none;
  }
  .label {
    position: absolute;
    top: 8px;
    left: 14px;
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: color-mix(in srgb, var(--group-tint) 70%, var(--dim));
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .count {
    display: inline-block;
    min-width: 18px;
    padding: 0 5px;
    text-align: center;
    border-radius: var(--radius-pill);
    background: color-mix(in srgb, var(--group-tint) 25%, var(--border));
    color: var(--fg);
    font-size: var(--text-xs);
    line-height: 16px;
  }
  /* Re-enable pointer events for the controls — the .feature-group container
     is click-through so canvas drags pass to children. */
  .group-actions {
    pointer-events: auto;
    display: inline-flex;
    gap: 4px;
    margin-left: 2px;
  }
  .gbtn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    padding: 0;
    border: 1px solid color-mix(in srgb, var(--group-tint) 45%, var(--border));
    border-radius: 4px;
    background: color-mix(in srgb, var(--group-tint) 18%, var(--card));
    color: color-mix(in srgb, var(--group-tint) 75%, var(--fg));
    cursor: pointer;
    transition: background 0.15s, color 0.15s, opacity 0.15s;
  }
  .gbtn:hover:not(:disabled) {
    background: color-mix(in srgb, var(--group-tint) 38%, var(--card));
    color: var(--fg);
  }
  .gbtn:disabled {
    opacity: 0.45;
    cursor: default;
  }
</style>
