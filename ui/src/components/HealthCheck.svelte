<script lang="ts">
  import { store } from '$lib/stores.svelte'
  import { runDoctorChecks } from '$lib/doctor'
  import { Check, Info, Minus, TriangleAlert, X } from '@lucide/svelte'

  const statusLabels: Record<string, string> = {
    pass: 'passed',
    fail: 'failed',
    warn: 'warning',
    info: 'info',
  }
</script>

<div class="doctor-section">
  <div class="doctor-toolbar">
    <button
      disabled={store.daemon.doctorRunning}
      aria-busy={store.daemon.doctorRunning}
      onclick={() => runDoctorChecks()}
    >
      {store.daemon.doctorRunning ? 'Running...' : 'Run Checks'}
    </button>
    {#if store.daemon.doctorRanAt}
      <span class="doctor-timestamp">Last: {store.daemon.doctorRanAt}</span>
    {/if}
    {#if store.daemon.doctorChecks.length}
      <span class="doctor-summary">
        {#if store.daemon.doctorPassCount}<span class="pass">{store.daemon.doctorPassCount} pass</span>{/if}
        {#if store.daemon.doctorFailCount}<span class="fail">{store.daemon.doctorFailCount} fail</span>{/if}
        {#if store.daemon.doctorWarnCount}<span class="warn">{store.daemon.doctorWarnCount} warn</span>{/if}
      </span>
    {/if}
  </div>

  {#if store.daemon.doctorChecks.length}
    {#each store.daemon.doctorChecks as c (c.name)}
      <div class="doctor-check">
        <span
          class="doctor-icon {c.status}"
          role="status"
          aria-label={statusLabels[c.status] ?? c.status}
        >
          {#if c.status === 'pass'}
            <Check size={14} strokeWidth={2.4} />
          {:else if c.status === 'fail'}
            <X size={14} strokeWidth={2.4} />
          {:else if c.status === 'warn'}
            <TriangleAlert size={14} strokeWidth={2.2} />
          {:else if c.status === 'info'}
            <Info size={14} strokeWidth={2.2} />
          {:else}
            <Minus size={14} strokeWidth={2.2} />
          {/if}
        </span>
        <span class="doctor-text">
          <span class="name">{c.name}</span>
          <span class="msg">{c.message}</span>
        </span>
      </div>
    {/each}
  {:else if !store.daemon.doctorRunning}
    <div class="doctor-empty">No results yet.</div>
  {/if}
</div>

<style>
  .doctor-section {
    margin: 0 var(--space-5) var(--space-4);
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    overflow: hidden;
  }
  .doctor-toolbar {
    padding: 0.6rem var(--space-4); /* 0.6rem off-grid vertical */
    display: flex;
    align-items: center;
    gap: var(--space-4);
    border-bottom: 1px solid var(--border);
  }
  .doctor-timestamp {
    font-size: var(--text-sm);
    color: var(--dim);
  }
  .doctor-check {
    display: grid;
    grid-template-columns: 2rem 1fr;
    align-items: baseline;
    gap: 0.3rem; /* off-grid: intentional tight grid gutter */
    padding: 0.4rem var(--space-4); /* 0.4rem off-grid vertical */
    border-top: 1px solid var(--border);
  }
  .doctor-check:first-child {
    border-top: none;
  }
  .doctor-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-weight: 700;
    font-size: var(--text-md);
  }
  .doctor-icon.pass { color: var(--green); }
  .doctor-icon.fail { color: var(--red); }
  .doctor-icon.warn { color: var(--yellow); }
  .doctor-icon.info { color: var(--dim); }
  .doctor-text {
    font-size: var(--text-md);
  }
  .doctor-text .name {
    font-weight: 600;
  }
  .doctor-text .msg {
    color: var(--dim);
    margin-left: var(--space-2);
  }
  .doctor-empty {
    padding: var(--space-3) var(--space-4);
    color: var(--dim);
    font-size: var(--text-md);
  }
  .doctor-summary {
    margin-left: auto;
    display: flex;
    gap: var(--space-2);
    font-size: var(--text-sm);
  }
  .doctor-summary .pass { color: var(--green); }
  .doctor-summary .fail { color: var(--red); }
  .doctor-summary .warn { color: var(--yellow); }
</style>
