<script lang="ts">
  type Props = { health: string; targetName?: string; environmentName?: string; onOpenService: () => void }
  let { health, targetName, environmentName, onOpenService }: Props = $props()
  const healthy = $derived(health === 'healthy')
</script>
{#if healthy}
  <section class="context" aria-label="SQL Server context">
    <div><small>SQL Server</small><strong class="health"><span aria-hidden="true">●</span> Healthy</strong></div>
    <div><small>Target</small><strong>{targetName || 'Unknown'}</strong></div>
    <div><small>Environment</small><strong>{environmentName || 'Unknown'}</strong></div>
  </section>
{:else}
  <section class="degraded" role="status"><strong>{targetName || 'SQL Server target'} is {health || 'not healthy'}.</strong><span>Schema checks, publishing, and resets are unavailable.</span><button type="button" onclick={onOpenService}>Open target</button></section>
{/if}
<style>
  .context { display: grid; grid-template-columns: auto auto 1fr; align-items: center; border: 1px solid var(--border); border-radius: var(--radius-lg); background: var(--card); overflow: hidden; }
  .context > div { padding: var(--space-3) var(--space-4); border-right: 1px solid var(--border); min-width: 0; display: flex; flex-direction: column; gap: 3px; }
  small { color: var(--dim); text-transform: uppercase; font-size: var(--text-xs); font-weight: 700; } strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .health { color: var(--green); }
  .degraded { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-3); padding: var(--space-3) var(--space-4); border: 1px solid color-mix(in srgb, var(--yellow) 45%, var(--border)); border-radius: var(--radius-lg); background: color-mix(in srgb, var(--yellow) 8%, var(--card)); } .degraded span { color: var(--text-secondary); } .degraded button { margin-left: auto; }
  @media (max-width: 560px) { .degraded button { margin-left: 0; } }
  @media (max-width: 800px) { .context { grid-template-columns: 1fr 1fr; } }
</style>
