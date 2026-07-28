<script lang="ts">
  import type { ResourceStatus, EnvToggleInfo } from '../lib/types.gen'
  import { store, toast } from '$lib/stores.svelte'
  import { apiPut, setEnvToggle } from '../lib/api'
  import { tooltip } from '../lib/tooltip.svelte'

  let { svc, readOnly = false }: { svc: ResourceStatus; readOnly?: boolean } = $props()

  const stopped = $derived(svc.state === 'stopped')
  const toggles = $derived(store.daemon.envToggles.filter(t => t.service === svc.name))

  async function handleEnvToggle(t: EnvToggleInfo) {
    if (readOnly) return
    const newState = !t.enabled
    t.enabled = newState
    const { ok, data } = await setEnvToggle(t.service, t.var, newState)
    if (ok) {
      toast(data?.message || `${t.label} ${newState ? 'ON' : 'OFF'}`)
    } else {
      t.enabled = !newState
      toast(data?.error || 'Failed')
    }
  }

  const hasMode = $derived(!!svc.mode)
  const isContainerMode = $derived(svc.mode === 'container')

  async function handleModeToggle() {
    if (readOnly) return
    const newMode = isContainerMode ? 'dev' : 'container'
    const { ok, data } = await apiPut(`/api/service-mode/${svc.name}`, { mode: newMode })
    if (ok) {
      toast(data?.message || `${svc.name} → ${newMode} mode`)
    } else {
      toast(data?.error || 'Failed to switch mode')
    }
  }

</script>

{#if hasMode}
  <div class="mode-toggle-row">
    <div class="mode-toggle">
      <button class:active={!isContainerMode} disabled={readOnly} onclick={handleModeToggle} use:tooltip={{ content: 'Run as local process (dotnet watch / pnpm dev)' }}>Dev</button>
      <button class:active={isContainerMode} disabled={readOnly} onclick={handleModeToggle} use:tooltip={{ content: 'Run as Docker container from remote image' }}>Container</button>
    </div>
  </div>
{/if}

{#if (!stopped && svc.startup_time) || (!stopped && svc.uptime) || svc.restart_count > 0}
  <div class="card-meta">
    {#if !stopped && svc.startup_time}
      <span class="timing">started in {svc.startup_time}</span>
    {/if}
    {#if !stopped && svc.uptime}
      <span class="timing">up {svc.uptime}</span>
    {/if}
    {#if svc.restart_count > 0}
      <span class="timing">restarts: {svc.restart_count}</span>
    {/if}
  </div>
{/if}

{#if toggles.length}
  <div class="card-toggles">
    {#each toggles as toggle (toggle.var)}
      <div class="toggle-row" use:tooltip={{ content: `${toggle.var} = ${toggle.enabled ? toggle.value : '(unset, fallback to .env)'}` }}>
        <div class="toggle-info">
          <span class="toggle-label">{toggle.label}</span>
          <span class="toggle-desc">{toggle.description}</span>
        </div>
        <button
          class="toggle-switch"
          class:on={toggle.enabled}
          disabled={readOnly}
          onclick={() => handleEnvToggle(toggle)}
          aria-label={`${toggle.label}: ${toggle.enabled ? 'on' : 'off'}`}
          aria-pressed={toggle.enabled}
        >
          <span class="toggle-knob"></span>
        </button>
      </div>
    {/each}
  </div>
{/if}

<style>
  .mode-toggle-row {
    display: flex;
    justify-content: flex-end;
    margin-bottom: var(--space-3);
  }
  .mode-toggle {
    display: flex;
    border: 1px solid var(--border);
    border-radius: 5px; /* off-grid: between radius-sm(4) and radius-md(6) */
    overflow: hidden;
    flex-shrink: 0;
  }
  .mode-toggle button {
    background: none;
    border: none;
    color: var(--dim);
    padding: 0.2rem 0.55rem; /* off-grid: intentional compact segment */
    font-size: var(--text-xs);
    cursor: pointer;
    min-height: auto;
  }
  .mode-toggle button:not(:last-child) {
    border-right: 1px solid var(--border);
  }
  .mode-toggle button.active {
    background: var(--blue);
    color: var(--white);
  }
  .mode-toggle button:hover:not(.active) {
    background: rgba(255, 255, 255, 0.05);
  }
  .card-meta {
    padding: var(--space-2) 0;
    font-size: var(--text-md);
    color: var(--dim);
    display: flex;
    flex-wrap: wrap;
    gap: 0.3rem var(--space-4); /* 0.3rem off-grid: intentional tight vertical gap */
  }
  .timing {
    color: var(--dim);
  }
  .card-toggles {
    padding: var(--space-2) 0;
  }
  .toggle-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    padding: 0.2rem 0; /* off-grid: intentional minimal row padding */
  }
  .toggle-info {
    display: flex;
    flex-direction: column;
    gap: 0.1rem; /* off-grid: intentional tight label stack */
  }
  .toggle-label {
    font-size: var(--text-md);
    color: var(--fg);
  }
  .toggle-desc {
    font-size: var(--text-xs);
    color: var(--dim);
  }
  .toggle-switch {
    position: relative;
    width: 36px;
    height: 20px;
    border-radius: 10px; /* off-grid: pill for switch track */
    background: var(--border);
    border: none;
    cursor: pointer;
    padding: 0;
    min-height: auto;
    flex-shrink: 0;
    transition: background 0.2s;
  }
  .toggle-switch.on {
    background: var(--green);
  }
  .toggle-knob {
    position: absolute;
    top: 3px; /* 1px/2px/3px border exception */
    left: 3px; /* 1px/2px/3px border exception */
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: var(--white);
    transition: transform 0.2s;
  }
  .toggle-switch.on .toggle-knob {
    transform: translateX(16px);
  }
</style>
