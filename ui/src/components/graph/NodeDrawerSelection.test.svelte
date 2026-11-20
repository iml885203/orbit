<script lang="ts">
  import NodeDrawer from './NodeDrawer.svelte'
  import { store } from '$lib/stores.svelte'
  const node = $derived(store.graph.active?.nodes.find((item) => item.name === store.graph.selectedNode) ?? null)
</script>

<NodeDrawer {node} onClose={() => { store.graph.selectedNode = null }} />
