<script lang="ts">
  import { playback } from '../../lib/tracePlayback.svelte'
  import { SkipBack, SkipForward } from '@lucide/svelte'

  // Label for the current step: the root entry at -1, otherwise the hop.
  const stepLabel = $derived.by(() => {
    if (playback.current < 0) return `entry · ${playback.rootService}`
    const s = playback.steps[playback.current]
    return s ? `${s.from} → ${s.to}` : ''
  })
  const stepError = $derived(playback.current >= 0 && playback.steps[playback.current]?.error)
  const counter = $derived(
    playback.current < 0 ? 'root' : `${playback.current + 1} / ${playback.steps.length}`,
  )
</script>

{#if playback.active}
  <div class="playback" role="group" aria-label="Trace playback">
    <span class="pill pill-info">Trace</span>
    <span class="tid">{playback.traceId.slice(0, 8)}…</span>
    <span class="sep"></span>
    <button class="ctl" aria-label="Previous step" disabled={playback.current <= -1} onclick={() => playback.prev()}>
      <SkipBack size={15} />
    </button>
    <span class="step" class:err={stepError}>
      <span class="lbl">{stepLabel}</span>
      <span class="cnt">{counter}</span>
    </span>
    <button class="ctl" aria-label="Next step" disabled={playback.current >= playback.steps.length - 1} onclick={() => playback.next()}>
      <SkipForward size={15} />
    </button>
    {#if stepError}<span class="pill pill-err" role="status">Error</span>{/if}
    <span class="sep"></span>
    <button class="exit" onclick={() => playback.exit()}>Exit playback</button>
  </div>
{/if}

<style>
  .playback {
    position: absolute;
    bottom: var(--space-4);
    left: 50%;
    transform: translateX(-50%);
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    background: color-mix(in srgb, var(--card) 96%, transparent);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: 0 8px 28px rgba(0, 0, 0, 0.4);
    z-index: 40;
    font-size: var(--text-md);
  }
  .tid { font-family: var(--font-mono); color: var(--dim); }
  .sep { width: 1px; height: 18px; background: var(--border); }
  .ctl {
    display: inline-flex; align-items: center; justify-content: center;
    width: 28px; height: 28px; padding: 0;
    background: transparent; border: 1px solid var(--border);
    border-radius: var(--radius-md); color: var(--fg); cursor: pointer;
  }
  .ctl:hover:not(:disabled) { border-color: color-mix(in srgb, var(--blue) 50%, var(--border)); color: var(--blue); }
  .ctl:disabled { opacity: 0.4; cursor: default; }
  .step { display: inline-flex; flex-direction: column; align-items: center; min-width: 130px; }
  .step .lbl { font-family: var(--font-mono); }
  .step.err .lbl { color: var(--red); }
  .step .cnt { font-size: var(--text-xs); color: var(--dim); }
  .exit {
    background: transparent; border: 1px solid var(--border);
    color: var(--dim); border-radius: var(--radius-md);
    padding: var(--space-1) var(--space-3); cursor: pointer; font-family: inherit;
    min-height: var(--hit-target);
  }
  .exit:hover { color: var(--fg); border-color: var(--fg); }
</style>
