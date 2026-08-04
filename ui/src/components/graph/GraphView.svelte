<script lang="ts">
  import { SvelteFlow, SvelteFlowProvider, Background, Controls, Panel, type Edge } from '@xyflow/svelte'
  import '@xyflow/svelte/dist/style.css'
  import { store } from '../../lib/stores.svelte'
  import { layout } from './layout'
  import { Activity } from '@lucide/svelte'
  import { tooltip } from '../../lib/tooltip.svelte'
  import { liveTraffic } from '../../lib/liveTraffic.svelte'
  import { subscribe } from '../../lib/eventbus'
  import type { TraceSummary } from '../../lib/types.gen'
  import { dependencyEdgeID, routeVisibleDependencyEdges, type RoutedGraphEdge } from './edge-routing'
  import ServiceNode from './ServiceNode.svelte'
  import GroupNode from './GroupNode.svelte'
  import ExternalNode from './ExternalNode.svelte'
  import DependencyEdge from './DependencyEdge.svelte'
  import EnvSwitchingOverlay from './EnvSwitchingOverlay.svelte'
  import TracePlaybackBar from './TracePlaybackBar.svelte'

  type GraphEdgeData = RoutedGraphEdge & Record<string, unknown>

  // Canvas reads the store's `active` getter so the preview-wins rule
  // lives in one place (stores.svelte.ts GraphStore.active).
  const activeGraph = $derived(store.graph.active)
  const isPreviewing = $derived(store.graph.isPreviewing)
  const layoutMode = $derived(store.ui.layoutMode)
  // Toggle only matters when the env has groups to lay out.

  const nodes = $derived<ReturnType<typeof layout>>(activeGraph ? layout(activeGraph, layoutMode) : [])

  const selectedNode = $derived(store.graph.selectedNode)
  const edges = $derived<Edge<GraphEdgeData, 'dep'>[]>(
    routeVisibleDependencyEdges(activeGraph?.edges ?? [], selectedNode, nodes)
      .map(e => ({
        id: dependencyEdgeID(e),
        source: e.from,
        target: e.to,
        type: 'dep',
        data: e as GraphEdgeData,
      }))
  )

  const nodeTypes = { service: ServiceNode, group: GroupNode, external: ExternalNode }
  const edgeTypes = { dep: DependencyEdge }

  function onNodeClick({ node }: { node: { id: string }; event: MouseEvent | TouchEvent }) {
    store.graph.selectedNode = node.id
  }

  // Live ambient mode: while enabled, feed each incoming trace's services into
  // the liveTraffic window so edges pulse recent activity. Subscription is only
  // open while the toggle is on.
  $effect(() => {
    if (!liveTraffic.enabled) return
    const unsub = subscribe('trace', (data) => liveTraffic.note((data as TraceSummary).services ?? []))
    return unsub
  })
</script>

{#if !activeGraph}
  <p class="empty">Loading graph…</p>
{:else if activeGraph.nodes.length === 0}
  <p class="empty">No services in this env. Edit envs/{activeGraph.env}.yaml to add some.</p>
{:else}
  <div class="canvas">
    <SvelteFlowProvider>
      <SvelteFlow
        {nodes}
        {edges}
        {nodeTypes}
        {edgeTypes}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={true}
        fitView
        proOptions={{ hideAttribution: true }}
        onnodeclick={onNodeClick}
      >
        <Background />
        <Controls showLock={false} />
        <Panel position="top-left">
          <button
            class="live-btn"
            class:active={liveTraffic.enabled}
            aria-pressed={liveTraffic.enabled}
            aria-label="Live traffic"
            use:tooltip={{ content: 'Live — pulse edges carrying recent traces' }}
            onclick={() => liveTraffic.toggle()}
          >
            <Activity size={14} strokeWidth={2.25} />
            <span>Live</span>
          </button>
        </Panel>
      </SvelteFlow>
    </SvelteFlowProvider>
    {#if isPreviewing}
      <div class="preview-hint" role="status">
        Previewing <strong>{store.graph.preview!.env}</strong> — actions disabled.
      </div>
    {/if}

    <EnvSwitchingOverlay />
    <TracePlaybackBar />
  </div>
{/if}

<style>
  .canvas {
    flex: 1 1 auto;
    width: 100%;
    min-height: 0;
    position: relative;
  }
  .empty { padding: var(--space-5); color: var(--dim); }

  .preview-hint {
    position: absolute;
    top: var(--space-3);
    left: 50%;
    transform: translateX(-50%);
    padding: var(--space-1) var(--space-3);
    background: var(--card);
    border: 1px solid var(--blue);
    border-radius: var(--radius-pill);
    color: var(--fg);
    font-size: var(--text-md);
    z-index: 30;
    pointer-events: none;
  }
  .preview-hint strong { font-family: var(--font-mono); color: var(--blue); }

  .live-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: 4px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--card);
    color: var(--dim);
    cursor: pointer;
    font-size: var(--text-sm);
    font-family: inherit;
  }
  .live-btn:hover { color: var(--fg); border-color: color-mix(in srgb, var(--fg) 20%, var(--border)); }
  .live-btn.active {
    color: var(--green);
    border-color: color-mix(in srgb, var(--green) 45%, var(--border));
    background: color-mix(in srgb, var(--green) 10%, var(--card));
  }
</style>
