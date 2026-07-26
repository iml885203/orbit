<script lang="ts">
  // ExternalNode renders a third-party / off-platform dependency declared
  // in yaml with `kind: external` (e.g. an upstream "upstream" we don't own).
  // It deliberately looks different from ServiceNode: smaller footprint,
  // dashed border, no action affordances — it's a reference, not something
  // orbit can start/stop.
  import { Handle, Position } from '@xyflow/svelte'
  import type { GraphNode } from '../../lib/types.gen'
  import { hashHue } from '../../lib/hash'

  type Props = { data: GraphNode }
  let { data }: Props = $props()

  // Same name lands on the same hue as GroupNode (both use hashHue).
  const tint = $derived(data.color ?? `hsl(${hashHue(data.name)} 55% 60%)`)
  const label = $derived(data.label ?? data.name)
</script>

<div class="external" style:--tint={tint}>
  <Handle type="target" position={Position.Top} style="opacity: 0;" />
  <span class="label">{label}</span>
  <span class="badge">ext</span>
  <Handle type="source" position={Position.Bottom} style="opacity: 0;" />
</div>

<style>
  .external {
    width: 160px;
    min-height: 56px;
    border: 1px dashed var(--tint);
    border-radius: var(--radius-md, 8px);
    background: color-mix(in srgb, var(--tint) 8%, var(--card));
    color: var(--fg);
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    justify-content: center;
    padding: 8px 12px;
    font-family: var(--font-mono);
    font-size: var(--text-xs, 12px);
    box-sizing: border-box;
  }
  .label {
    font-weight: 600;
    color: var(--tint);
  }
  .badge {
    font-size: 9px;
    opacity: 0.6;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    margin-top: 4px;
  }
</style>
