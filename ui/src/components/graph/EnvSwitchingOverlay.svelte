<script lang="ts">
  import { store } from '../../lib/stores.svelte'
</script>

{#if store.graph.envSwitching}
  {@const sw = store.graph.envSwitching}
  {@const stillRunning = Object.values(store.daemon.services).filter(
    s => s.state !== 'stopped' && s.state !== 'pending'
  ).length}
  {@const stopped = Math.max(0, Math.min(sw.total, sw.total - stillRunning))}
  {@const pct = sw.total > 0 ? Math.round((stopped / sw.total) * 100) : 0}
  <div class="switching-overlay" role="status" aria-live="polite">
    <div class="switching-card">
      <div class="row">
        <div class="spinner" aria-hidden="true"></div>
        <span>
          {#if sw.phase === 'stopping'}
            Stopping previous env…
          {:else if sw.phase === 'stalled'}
            Still waiting for <strong>{sw.target}</strong> — the daemon may be restarting…
          {:else}
            Loading <strong>{sw.target}</strong>…
          {/if}
        </span>
      </div>
      {#if sw.phase === 'stopping' && sw.total > 0}
        <div class="progress" aria-label="Stopping services">
          <div class="progress-bar" style:width="{pct}%"></div>
        </div>
        <div class="progress-label">
          Stopped {stopped} of {sw.total}
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .switching-overlay {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(13, 17, 23, 0.7);
    backdrop-filter: blur(2px);
    z-index: 40;
  }
  .switching-card {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-4);
    min-width: 280px;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
    color: var(--fg);
    font-size: var(--text-base);
  }
  .switching-card .row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }
  .switching-card strong { font-family: monospace; }
  .progress {
    width: 100%;
    height: 4px;
    background: var(--border);
    border-radius: var(--radius-pill);
    overflow: hidden;
  }
  .progress-bar {
    height: 100%;
    background: var(--blue);
    border-radius: var(--radius-pill);
    transition: width 0.3s ease-out;
  }
  .progress-label {
    font-size: var(--text-sm);
    color: var(--dim);
    font-family: monospace;
  }
  .spinner {
    width: 16px;
    height: 16px;
    border: 2px solid var(--border);
    border-top-color: var(--blue);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to { transform: rotate(360deg); }
  }
  @media (prefers-reduced-motion: reduce) {
    .spinner { animation: none; }
  }
</style>
