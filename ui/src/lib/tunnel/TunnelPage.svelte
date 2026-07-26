<script lang="ts">
  import { onMount } from 'svelte'
  import { fetchTunnels, type TunnelView } from './api'
  import TunnelClaimForm from './TunnelClaimForm.svelte'
  import TunnelPipeline from './TunnelPipeline.svelte'
  import GlobalClaims from './GlobalClaims.svelte'

  let tunnels = $state<TunnelView[]>([])
  let gateway = $state('')
  let loaded = $state(false)

  const healthy = $derived(tunnels.filter(t => t.status === 'healthy').length)
  const pending = $derived(
    tunnels.filter(t => t.status === 'connecting' || t.status === 'reconnecting').length,
  )
  const errored = $derived(tunnels.filter(t => t.status === 'error').length)

  // Display-only: a schemeless gateway value must degrade, not throw
  // inside $derived and take the page down.
  const gatewayName = $derived(gateway ? (URL.canParse(gateway) ? new URL(gateway).host : gateway) : '')

  async function refresh() {
    const res = await fetchTunnels()
    tunnels = res.tunnels
    gateway = res.gateway ?? ''
    loaded = true
  }

  onMount(() => {
    refresh()
    // No SSE topic for tunnels; poll. Tunnels are few and the page is desktop-only,
    // so a 2s cadence is cheap and keeps the status/proxyPort fresh through reconnects.
    const id = setInterval(refresh, 2000)
    return () => clearInterval(id)
  })
</script>

<div class="tunnel-page">
  <header class="head">
    <h2 class="page-title">Tunnels</h2>
    {#if gatewayName}
      <span class="gateway" role="status" title={gateway}>
        <span class="gateway-dot" aria-hidden="true"></span>
        {gatewayName}
      </span>
    {/if}
    <span class="spacer"></span>
    {#if loaded && tunnels.length > 0}
      <div class="summary" aria-label="Tunnel summary">
        {#if healthy}<span class="stat healthy">{healthy} healthy</span>{/if}
        {#if pending}<span class="stat pending">{pending} connecting</span>{/if}
        {#if errored}<span class="stat error">{errored} error</span>{/if}
      </div>
    {/if}
  </header>

  {#if loaded}<TunnelClaimForm onClaimed={refresh} />{/if}

  {#if !loaded}
    <p class="hint">Loading…</p>
  {:else if tunnels.length === 0}
    <div class="empty">
      <div class="empty-wire" aria-hidden="true">
        <span class="dot"></span><span class="line"></span><span class="dot"></span>
      </div>
      <p class="empty-title">No active tunnels</p>
      <p class="empty-sub">Create a route above to start receiving staging callbacks.</p>
    </div>
  {:else}
    <ul class="list">
      {#each tunnels as t (t.local_port)}
        <li><TunnelPipeline tunnel={t} /></li>
      {/each}
    </ul>
  {/if}

  {#if loaded}
    <GlobalClaims />
  {/if}
</div>

<style>
  .tunnel-page {
    /* Plain content flow — the app shell's <main> owns scrolling/sizing
       (mirrors HealthCheckPage). Don't set flex/height/overflow here or the
       page over-grows and covers the header. */
    padding-bottom: var(--space-6);
  }
  .head {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin: var(--space-4) var(--space-5) var(--space-2);
  }
  .page-title {
    font-size: var(--text-md);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--dim);
    font-weight: 600;
  }
  .spacer { flex: 1 1 auto; }
  /* The claim-owning gateway is operational context, so keep it visible. */
  .gateway {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    color: var(--fg);
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-pill);
    padding: 2px var(--space-3);
    cursor: default;
  }
  .gateway-dot {
    width: 7px; height: 7px;
    border-radius: var(--radius-pill);
    background: var(--blue);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--blue) 18%, transparent);
  }
  .summary { display: flex; gap: var(--space-3); }
  .stat {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .stat.healthy { color: var(--green); }
  .stat.pending { color: var(--blue); }
  .stat.error { color: var(--red); }

  .hint {
    color: var(--dim);
    font-size: var(--text-base);
    margin: var(--space-5);
  }

  .list {
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    /* Cap width so each tunnel reads as a contained card, not a full-bleed band
       stretched across a wide monitor (the wire would otherwise span ~900px). */
    max-width: 900px;
    margin: var(--space-2) var(--space-5) 0;
  }
  /* Flex items default to min-width:auto (= min-content); the pipeline's grid has
     wide content, so without this the <li> grows past the list cap and overflows. */
  .list > li {
    min-width: 0;
  }

  .empty {
    margin: var(--space-6) auto;
    max-width: 460px;
    text-align: center;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-3);
  }
  .empty-wire { display: flex; align-items: center; gap: var(--space-2); opacity: 0.5; }
  .empty-wire .dot {
    width: 8px; height: 8px; border-radius: var(--radius-pill);
    background: var(--dim);
  }
  .empty-wire .line {
    width: 120px; height: 2px;
    background: repeating-linear-gradient(90deg, var(--dim) 0 4px, transparent 4px 8px);
  }
  .empty-title { font-size: var(--text-lg); color: var(--fg); font-weight: 600; }
  .empty-sub { font-size: var(--text-base); color: var(--dim); }
</style>
