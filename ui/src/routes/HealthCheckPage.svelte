<script lang="ts">
  import { onMount } from 'svelte'
  import HealthCheck from '../components/HealthCheck.svelte'
  import { runDoctorChecks } from '$lib/doctor'

  onMount(() => {
    runDoctorChecks({ ifStale: true })
  })
</script>

<h2 class="page-title">Health Check</h2>
<HealthCheck />

<style>
  .page-title {
    font-size: var(--text-md);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--dim);
    margin: var(--space-4) var(--space-5) var(--space-2);
    font-weight: 600;
  }
</style>
