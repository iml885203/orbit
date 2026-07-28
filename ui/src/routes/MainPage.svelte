<script lang="ts">
  import EnvSwitcher from '../components/graph/EnvSwitcher.svelte'
  import GraphView from '../components/graph/GraphView.svelte'
  import NodeDrawer from '../components/graph/NodeDrawer.svelte'
  import EdgeInfoPopover from '../components/graph/EdgeInfoPopover.svelte'
  import OperationBanner from '../components/graph/OperationBanner.svelte'
  import LogModal from '../components/LogModal.svelte'
  import { push } from 'svelte-spa-router'
  import { hydrateLogs, store } from '$lib/stores.svelte'
  import { fetchLogs } from '$lib/api'

  const target = $derived(store.daemon.logModal.target)
  const lines = $derived(target ? (store.daemon.logBuffers[target] ?? []) : [])
  const selectedNodeData = $derived(
    store.graph.selectedNode && store.graph.data
      ? store.graph.data.nodes.find(n => n.name === store.graph.selectedNode) ?? null
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
  <GraphView />
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
    display: flex;
    flex: 1 1 auto;
    flex-direction: column;
    min-height: 0;
    position: relative; /* anchors OperationBanner */
  }
</style>
