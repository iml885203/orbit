<script lang="ts">
  import { push } from 'svelte-spa-router'
  import { Stethoscope } from '@lucide/svelte'
  import { store, toast } from '$lib/stores.svelte'
  import { settingsSections } from '$ext'
  import { apiPut } from '$lib/api'
  import type { SettingsResponse } from '$lib/types.gen'

  async function setShowHistory(show: boolean) {
    store.ui.showHistory = show
    const { ok, data } = await apiPut<SettingsResponse>('/api/settings', {
      show_history: show,
    })
    if (ok) {
      toast(show ? 'History bar shown' : 'History bar hidden')
    } else {
      store.ui.showHistory = !show
      toast(data?.error || 'Failed to update settings')
    }
  }

	async function setAutomaticUpdates(policy: 'automatic' | 'off') {
		const previous = store.ui.automaticUpdates
		store.ui.automaticUpdates = policy
		const { ok, data } = await apiPut<SettingsResponse>('/api/settings', {
			automatic_updates: policy,
		})
		if (ok) {
			toast(policy === 'automatic' ? 'Automatic updates enabled' : 'Automatic updates disabled')
		} else {
			store.ui.automaticUpdates = previous
			toast(data?.error || 'Failed to update automatic updates')
		}
	}

  function openDiagnostics() {
    store.ui.settingsOpen = false
    void push('/healthcheck')
  }
</script>

<svelte:window onkeydown={(e) => { if (store.ui.settingsOpen && e.key === 'Escape') store.ui.settingsOpen = false }} />

{#if store.ui.settingsOpen}
  <button
    type="button"
    class="settings-overlay"
    aria-label="Close settings"
    onclick={() => store.ui.settingsOpen = false}
  ></button>
  <div class="settings-popover" role="dialog" aria-label="Settings">
    <div class="settings-title">Settings</div>
    {#each settingsSections as Section, i (i)}
      <Section />
    {/each}
    <div class="setting-row">
		<div>
			<div class="setting-label">Automatic Updates</div>
			<div class="setting-desc">Check, verify, and apply when product resources are stopped</div>
		</div>
		<div class="toggle-group">
			<button class:active={store.ui.automaticUpdates === 'off'} onclick={() => setAutomaticUpdates('off')}>Off</button>
			<button class:active={store.ui.automaticUpdates === 'automatic'} onclick={() => setAutomaticUpdates('automatic')}>On</button>
		</div>
	</div>
	<div class="setting-row">
      <div>
        <div class="setting-label">Command History</div>
        <div class="setting-desc">
          {store.ui.showHistory ? 'Showing recent Orbit commands' : 'Hidden from the dashboard'}
        </div>
      </div>
      <div class="toggle-group">
        <button class:active={!store.ui.showHistory} onclick={() => setShowHistory(false)}>Hide</button>
        <button class:active={store.ui.showHistory} onclick={() => setShowHistory(true)}>Show</button>
      </div>
    </div>
    <div class="setting-row diagnostics">
      <div>
        <div class="setting-label">Diagnostics</div>
        <div class="setting-desc">Check tools, configuration, and local infrastructure</div>
      </div>
      <button class="open-diagnostics" type="button" onclick={openDiagnostics}><Stethoscope size={14} aria-hidden="true" /> Open</button>
    </div>
  </div>
{/if}

<style>
  .settings-overlay {
    position: fixed;
    inset: 0;
    z-index: 99;
    background: none;
    border: none;
    padding: 0;
    cursor: default;
  }
  .settings-popover {
    position: absolute;
    top: 3.2rem;
    right: var(--space-5);
    z-index: 100;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: 10px; /* off-grid: between --radius-lg(8px) and next step */
    padding: var(--space-4);
    min-width: 280px;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
  }
  .settings-title {
    font-size: var(--text-md);
    font-weight: 700;
    margin-bottom: var(--space-3);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--dim);
  }
  .setting-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    padding: var(--space-2) 0;
  }
  .setting-row.diagnostics {
    margin-top: var(--space-2);
    padding-top: var(--space-3);
    border-top: 1px solid var(--border);
  }
  .setting-label {
    font-size: var(--text-base);
  }
  .setting-desc {
    font-size: var(--text-sm);
    color: var(--dim);
    margin-top: 0.2rem; /* off-grid: intentional minimal push */
  }
  .toggle-group {
    display: flex;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  .toggle-group button {
    background: none;
    border: none;
    color: var(--dim);
    padding: 0.3rem 0.7rem; /* off-grid: intentional compact segment padding */
    font-size: var(--text-md);
    cursor: pointer;
  }
  .toggle-group button:not(:last-child) {
    border-right: 1px solid var(--border);
  }
  .toggle-group button.active {
    background: var(--blue);
    color: var(--white);
  }
  .toggle-group button:hover:not(.active) {
    background: rgba(255, 255, 255, 0.05);
  }
  .open-diagnostics {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    flex: none;
  }
</style>
