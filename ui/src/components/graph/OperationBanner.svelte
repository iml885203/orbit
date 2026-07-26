<script lang="ts">
  import { store } from '../../lib/stores.svelte'
  import { opProgress, snapshotOp } from '../../lib/opProgress.svelte'

  const LABEL = { up: 'Starting all services', infra: 'Starting infrastructure', down: 'Stopping everything' }

  const snap = $derived(
    opProgress.active
      ? snapshotOp(opProgress.active.kind, Object.values(store.daemon.services))
      : null,
  )

  // The daemon queues work and the first SSE tick lands up to ~2s later, so a
  // fresh operation may look "done" before anything started moving. Hold the
  // banner through that window; once movement is (or 5s have) passed, a done
  // snapshot is real — show the summary briefly, then clear.
  let sawMovement = $state(false)
  let clearTimer: ReturnType<typeof setTimeout> | undefined

  $effect(() => {
    if (!opProgress.active) { sawMovement = false; return }
    if (snap && snap.inFlight.length > 0) sawMovement = true
  })

  const settled = $derived(
    opProgress.active !== null && snap?.done === true &&
    (sawMovement || Date.now() - opProgress.active.startedAt > 5000),
  )

  $effect(() => {
    if (!settled) { clearTimeout(clearTimer); clearTimer = undefined; return }
    clearTimer = setTimeout(() => opProgress.clear(), 5000)
    return () => clearTimeout(clearTimer)
  })
</script>

{#if opProgress.active && snap}
  {@const kind = opProgress.active.kind}
  <div class="op-banner" role="status" aria-live="polite">
    {#if !settled}
      <span class="spinner" aria-hidden="true"></span>
      <span>{LABEL[kind]}</span>
      {#if snap.inFlight.length > 0}
        <span class="detail">waiting on <span class="mono">{snap.inFlight.slice(0, 4).join(', ')}{snap.inFlight.length > 4 ? ` +${snap.inFlight.length - 4}` : ''}</span></span>
      {/if}
    {:else if kind === 'down'}
      <span class="ok">✓ everything stopped</span>
    {:else if snap.degraded.length === 0}
      <span class="ok">✓ {snap.healthy} service{snap.healthy === 1 ? '' : 's'} healthy</span>
    {:else}
      <span class="bad">✗ {snap.degraded.length} degraded:</span>
      <span class="mono bad">{snap.degraded.map((d) => d.reason ? `${d.name} (${d.reason})` : d.name).slice(0, 3).join(' · ')}</span>
    {/if}
    <button type="button" class="dismiss" aria-label="Dismiss" onclick={() => opProgress.clear()}>✕</button>
  </div>
{/if}

<style>
  .op-banner {
    position: absolute;
    top: var(--space-3);
    left: 50%;
    transform: translateX(-50%);
    display: flex;
    align-items: center;
    gap: var(--space-2);
    max-width: min(720px, 90%);
    padding: var(--space-2) var(--space-3);
    background: color-mix(in srgb, var(--card) 96%, transparent);
    border: 1px solid var(--border);
    border-radius: var(--radius-pill);
    box-shadow: 0 6px 20px rgba(0, 0, 0, 0.35);
    font-size: var(--text-md);
    z-index: 35;
  }
  .mono { font-family: var(--font-mono); }
  .detail { color: var(--dim); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .ok { color: var(--green); }
  .bad { color: var(--red); }
  .spinner {
    width: 12px; height: 12px; flex-shrink: 0;
    border: 2px solid var(--border);
    border-top-color: var(--blue);
    border-radius: 50%;
    animation: opspin 0.8s linear infinite;
  }
  @keyframes opspin { to { transform: rotate(360deg); } }
  @media (prefers-reduced-motion: reduce) { .spinner { animation: none; } }
  .dismiss {
    background: none; border: 0; color: var(--dim); cursor: pointer;
    padding: 0 2px; font-size: var(--text-md);
  }
  .dismiss:hover { color: var(--fg); }
</style>
