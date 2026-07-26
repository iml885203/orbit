<script lang="ts">
  import { onMount } from 'svelte'
  import { devStore } from './stores.svelte'
  import { fetchDBProjects } from './api'
  import { publishDisabledReason } from './dbOpView.svelte'
  import DatabaseOperationsList from './DatabaseOperationsList.svelte'
  onMount(async () => { if (devStore.dbProjects.length === 0) { const p = await fetchDBProjects(); if (p) devStore.dbProjects = p } })
  const disabledReason = $derived(publishDisabledReason())
</script>
{#if devStore.dbProjects.length === 0}<p class="empty">No databases discovered. Run <code>orbit env sync</code> to refresh.</p>{:else}{#each devStore.dbProjects.filter((project) => project.databases.length > 0) as project (project.path)}<h4>{project.name}</h4><DatabaseOperationsList {project} states={devStore.dbState} operation={devStore.dbOpInFlight} {disabledReason} variant="drawer" />{/each}{/if}
<style>.empty { color: var(--dim); } h4 { margin: var(--space-3) 0 var(--space-1); color: var(--dim); font-size: var(--text-xs); text-transform: uppercase; letter-spacing: .05em; }</style>
