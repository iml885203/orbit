<script lang="ts">
  import EnvSwitcher from '../components/graph/EnvSwitcher.svelte'
  import GraphView from '../components/graph/GraphView.svelte'
  import ServiceTable from '../components/graph/ServiceTable.svelte'
  import NodeDrawer from '../components/graph/NodeDrawer.svelte'
  import EdgeInfoPopover from '../components/graph/EdgeInfoPopover.svelte'
  import OperationBanner from '../components/graph/OperationBanner.svelte'
  import LogModal from '../components/LogModal.svelte'
  import { push } from 'svelte-spa-router'
  import { hydrateLogs, store } from '$lib/stores.svelte'
  import { fetchLogs } from '$lib/api'
  import { List, Share2 } from '@lucide/svelte'
  import { tooltip } from '$lib/tooltip.svelte'

  const target = $derived(store.daemon.logModal.target)
  const lines = $derived(target ? (store.daemon.logBuffers[target] ?? []) : [])
  const activeGraph = $derived(store.graph.active)
  const selectedNodeData = $derived(
    store.graph.selectedNode && activeGraph
      ? activeGraph.nodes.find(n => n.name === store.graph.selectedNode) ?? null
      : null
  )

  let logRequest = 0
  $effect(() => {
    const service = target
    if (!service) return
    const request = ++logRequest
    fetchLogs(service).then((snapshot) => {
      if (request !== logRequest || store.daemon.logModal.target !== service) return
      if (snapshot) hydrateLogs(service, snapshot)
      store.daemon.logModal.loading = false
    })
  })
</script>

<section class="services-page" aria-label="Services">
  <EnvSwitcher />
  <div class="view-toolbar">
    <div class="view-toggle" role="group" aria-label="Services view">
      <button class:active={store.ui.serviceView === 'graph'} aria-pressed={store.ui.serviceView === 'graph'} aria-label="Graph view" use:tooltip={{ content: 'Graph view' }} onclick={() => store.ui.setServiceView('graph')}><Share2 size={15} /></button>
      <button class:active={store.ui.serviceView === 'table'} aria-pressed={store.ui.serviceView === 'table'} aria-label="Table view" use:tooltip={{ content: 'Table view' }} onclick={() => store.ui.setServiceView('table')}><List size={15} /></button>
    </div>
  </div>
  {#if store.ui.serviceView === 'table' && activeGraph}
    <ServiceTable graph={activeGraph} onSelect={(name) => { store.graph.selectedNode = name }} />
  {:else}
    <GraphView />
  {/if}
  <OperationBanner />
</section>

<NodeDrawer node={selectedNodeData} onClose={() => store.graph.selectedNode = null} />
<EdgeInfoPopover />

{#if target}
  <LogModal
    service={target}
    {lines}
    loading={store.daemon.logModal.loading}
    onClose={() => store.daemon.logModal.target = null}
    onOpenTrace={(id) => { store.daemon.logModal.target = null; push('/tracing/' + id) }}
  />
{/if}

<style>
  .services-page {
    --services-view-toggle-width: 64px;
    display: flex;
    flex: 1 1 auto;
    flex-direction: column;
    min-height: 0;
    position: relative; /* anchors OperationBanner */
  }
  .view-toolbar { position: absolute; top: 52px; right: var(--space-3); z-index: 20; }
  .view-toggle { display: inline-flex; gap: 2px; padding: 2px; border: 1px solid var(--border); border-radius: var(--radius-md); background: var(--card); }
  .view-toggle button { display: inline-flex; align-items: center; justify-content: center; width: 28px; height: 28px; padding: 0; border: 0; border-radius: var(--radius-sm); background: transparent; color: var(--dim); cursor: pointer; }
  .view-toggle button.active { background: color-mix(in srgb, var(--blue) 16%, transparent); color: var(--blue); }
</style>
