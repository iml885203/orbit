<script lang="ts">
  import { onMount } from 'svelte'
  import { gsap } from 'gsap'
  import type { SceneState } from './scene-state'
  import './DBTankScene.css'

  type Props = {
    state: SceneState
    projectName?: string
    progressPercent?: number
  }

  let {
    state: sceneState,
    projectName = 'Select a DB project',
    progressPercent = 0,
  }: Props = $props()

  let sceneEl: HTMLElement | undefined
  const ENERGY_RINGS = Array.from({ length: 5 })
  const DB_RINGS = Array.from({ length: 7 })
  const DB_RING_COUNT = DB_RINGS.length
  let selectionPulse = $state(false)
  let completePulse = $state(false)
  let failedPulse = $state(false)

  let previousProjectName: string | undefined
  let previousSceneState: SceneState | undefined
  let pulseTimers: ReturnType<typeof setTimeout>[] = []

  const clampedProgressPercent = $derived(clamp(progressPercent, 0, 100))
  const tankChargeScale = $derived(
    sceneState === 'complete' ? 1 : sceneState === 'building' ? clampedProgressPercent / 100 : 0,
  )
  const chargedRingCount = $derived(chargedRingCountFor())

  function prefersReducedMotion(): boolean {
    return (
      typeof window !== 'undefined' &&
      typeof window.matchMedia === 'function' &&
      window.matchMedia('(prefers-reduced-motion: reduce)').matches
    )
  }

  function clamp(value: number, min: number, max: number): number {
    return Math.min(max, Math.max(min, value))
  }

  function chargedRingCountFor(): number {
    if (sceneState === 'complete') return DB_RING_COUNT
    if (sceneState !== 'building' || clampedProgressPercent <= 0) return 0
    return Math.min(DB_RING_COUNT, Math.floor((clampedProgressPercent / 100) * DB_RING_COUNT))
  }

  function ringChargeFor(index: number): 'charged' | 'active' | 'pending' {
    const bottomOrdinal = DB_RING_COUNT - 1 - index
    if (bottomOrdinal < chargedRingCount) return 'charged'
    if (
      sceneState === 'building' &&
      clampedProgressPercent > 0 &&
      chargedRingCount < DB_RING_COUNT &&
      bottomOrdinal === chargedRingCount
    ) {
      return 'active'
    }
    return 'pending'
  }

  function triggerPulse(type: 'selection' | 'complete' | 'failed', duration: number) {
    if (prefersReducedMotion()) return
    if (type === 'selection') selectionPulse = false
    if (type === 'complete') completePulse = false
    if (type === 'failed') failedPulse = false

    requestAnimationFrame(() => {
      if (type === 'selection') selectionPulse = true
      if (type === 'complete') completePulse = true
      if (type === 'failed') failedPulse = true
      pulseTimers.push(
        setTimeout(() => {
          if (type === 'selection') selectionPulse = false
          if (type === 'complete') completePulse = false
          if (type === 'failed') failedPulse = false
        }, duration),
      )
    })
  }

  function clearPulseTimers() {
    pulseTimers.forEach((timer) => clearTimeout(timer))
    pulseTimers = []
  }

  $effect(() => {
    if (!sceneEl) return

    gsap.to(sceneEl, {
      '--charge-scale': tankChargeScale,
      duration: prefersReducedMotion() ? 0 : 1.1,
      ease: 'power2.inOut',
    })
  })

  $effect(() => {
    if (previousProjectName && projectName !== previousProjectName && sceneState !== 'building') {
      triggerPulse('selection', 900)
    }
    previousProjectName = projectName
  })

  $effect(() => {
    if (previousSceneState && sceneState !== previousSceneState) {
      if (sceneState === 'complete') triggerPulse('complete', 1400)
      if (sceneState === 'failed') triggerPulse('failed', 1250)
    }
    previousSceneState = sceneState
  })

  onMount(() => {
    return () => {
      clearPulseTimers()
    }
  })
</script>

<div
  bind:this={sceneEl}
  class={`tank-scene ${sceneState}`}
  class:selection-pulse={selectionPulse}
  class:complete-pulse={completePulse}
  class:failed-pulse={failedPulse}
  role="img"
  aria-label={`Local DB publish visualization${projectName ? ` for ${projectName}` : ''}`}
  style={`--charge-scale: ${tankChargeScale};`}
>
  <div class="scene-content">

    <div class="db-column-platform" aria-hidden="true">
      <span></span>
    </div>

    <div class="glass-tank db-energy-column" data-testid="db-tank-energy-column" aria-hidden="true">
      <div class="energy-rings" data-testid="db-tank-energy-rings">
        {#each ENERGY_RINGS as _, index (index)}
          <span style={`--ring-index: ${index};`}></span>
        {/each}
      </div>

      <svg
        class="db-column-svg"
        data-testid="db-tank-shell"
        viewBox="0 0 360 470"
        preserveAspectRatio="none"
        aria-hidden="true"
      >
        <defs>
          <linearGradient id="db-column-glass" x1="0" y1="0" x2="1" y2="0">
            <stop offset="0" stop-color="currentColor" stop-opacity="0.08" />
            <stop offset="0.52" stop-color="currentColor" stop-opacity="0.18" />
            <stop offset="1" stop-color="currentColor" stop-opacity="0.07" />
          </linearGradient>
          <linearGradient id="db-charge-beam" x1="0" y1="1" x2="0" y2="0">
            <stop offset="0" stop-color="currentColor" stop-opacity="0.52" />
            <stop offset="0.72" stop-color="currentColor" stop-opacity="0.2" />
            <stop offset="1" stop-color="currentColor" stop-opacity="0" />
          </linearGradient>
          <clipPath id="db-column-clip">
            <path d="M0 76 C0 42 314 42 314 76 L314 430 C314 464 0 464 0 430 Z" />
          </clipPath>
        </defs>

        <g class="db-column-glass">
          <path d="M0 76 C0 42 314 42 314 76 L314 430 C314 464 0 464 0 430 Z" />
          <ellipse class="db-column-rim db-column-rim-top" cx="157" cy="76" rx="157" ry="34" />
          <ellipse class="db-column-rim db-column-rim-inner" cx="157" cy="76" rx="139" ry="25" />
          <line class="db-column-side" x1="0" y1="76" x2="0" y2="430" />
          <line class="db-column-side" x1="314" y1="76" x2="314" y2="430" />
        </g>

        <g class="db-column-charge" clip-path="url(#db-column-clip)">
          <rect class="db-charge-beam" x="0" y="76" width="314" height="388" />
          <rect class="db-charge-core" x="133" y="76" width="48" height="388" />
        </g>

        <g class="db-ring-stack">
          {#each DB_RINGS as _, index (index)}
            {@const charge = ringChargeFor(index)}
            <g
              class={`db-ring db-ring-${charge}`}
              data-testid="db-tank-db-ring"
              data-charge={charge}
              data-line="solid"
              style={`--db-ring-index: ${index};`}
            >
              <ellipse class="db-ring-fill" cx="157" cy={94 + index * 56} rx="146" ry="27" />
              <ellipse class="db-ring-line" cx="157" cy={94 + index * 56} rx="146" ry="27" />
              <path class="db-ring-front" d={`M 24 ${94 + index * 56} C 72 ${112 + index * 56} 242 ${112 + index * 56} 290 ${94 + index * 56}`} />
            </g>
          {/each}
        </g>

        <ellipse class="db-column-base-glow" cx="157" cy="462" rx="170" ry="27" />
      </svg>
    </div>

  </div>
</div>
