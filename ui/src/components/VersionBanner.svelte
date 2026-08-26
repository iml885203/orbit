<script lang="ts">
  import { onDestroy } from 'svelte'
  import { apiPost, fetchVersion } from '$lib/api'
  import { store, toast } from '$lib/stores.svelte'
  import type { VersionRestartResponse } from '$lib/types.gen'

  type Phase = 'ready' | 'restarting' | 'success' | 'failed'

  let phase = $state<Phase>('ready')
  let targetVersion = $state('')
  let failure = $state('')
  let dismissedVersion = $state('')
  let successTimer: ReturnType<typeof setTimeout> | undefined

  const version = $derived(store.ui.version)
	const release = $derived(version?.release_update)
	const packageManaged = $derived(release?.owner === 'homebrew' || release?.owner === 'scoop')
	const cmd = $derived(release?.owner === 'homebrew' ? 'brew upgrade orbit' : release?.owner === 'scoop' ? 'scoop update orbit' : release?.target_version ? 'orbit update' : 'orbit daemon restart')
  const targetToken = $derived(version?.on_disk?.split(/\s+/)[0] ?? '')
  const runningToken = $derived(version?.running?.split(/\s+/)[0] ?? '')

  function wait(delay: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, delay))
  }

  function failRestart(message: string) {
    store.ui.versionRestarting = false
    failure = message
    phase = 'failed'
  }

  async function reconnect(expected: string) {
    for (const delay of [1000, 2000, 4000]) {
      await wait(delay)
      const controller = new AbortController()
      const deadline = setTimeout(() => controller.abort(), 2000)
      const next = await fetchVersion(controller.signal)
      clearTimeout(deadline)
      if (next?.running === expected) return next
    }
    return null
  }

  async function restart() {
		const expected = release?.target_version ?? version?.on_disk
    if (!expected) return
    targetVersion = expected
    failure = ''
    phase = 'restarting'
    store.ui.versionRestarting = true
		const endpoint = release?.target_version ? '/api/update/apply' : '/api/version/restart'
		const result = await apiPost<VersionRestartResponse>(endpoint)
    if (!result.ok) {
      failRestart(result.data?.error ?? 'Orbit could not schedule the restart.')
      return
    }
    const scheduled = result.data?.target_version
    if (!scheduled) {
      failRestart('Orbit did not report which version was scheduled.')
      return
    }
    targetVersion = scheduled
    const next = await reconnect(scheduled)
    if (!next) {
      failRestart('Orbit did not reconnect with the expected version.')
      return
    }
    store.ui.version = next
    store.ui.versionRestarting = false
    phase = 'success'
    clearTimeout(successTimer)
    successTimer = setTimeout(() => { phase = 'ready' }, 4000)
  }

  async function copyCmd() {
    try {
      await navigator.clipboard.writeText(cmd)
      toast('Command copied — paste into terminal')
    } catch {
      toast('Copy failed — ' + cmd)
    }
  }

  function dismiss() {
		const dismissTarget = release?.target_version ?? version?.on_disk
		if (dismissTarget) dismissedVersion = dismissTarget
  }

  onDestroy(() => clearTimeout(successTimer))
</script>

{#if phase === 'success'}
  <div class="banner success" role="status">
    <span class="text"><strong>Orbit updated to <code>{targetVersion.split(/\s+/)[0]}</code></strong></span>
  </div>
{:else if (version?.update_available || release?.phase === 'ready' || release?.phase === 'available' || release?.phase === 'partial' || release?.phase === 'failed') && dismissedVersion !== (release?.target_version ?? version?.on_disk)}
  <div class:error={phase === 'failed'} class="banner" role="status" aria-live="polite">
    {#if phase === 'restarting'}
      <span class="text"><strong>Restarting Orbit…</strong> Reconnecting to <code>{targetVersion.split(/\s+/)[0]}</code>.</span>
      <button disabled aria-busy="true">Restarting…</button>
    {:else if phase === 'failed'}
      <span class="text">
        <strong>Orbit didn’t restart.</strong> {failure}
      </span>
      <div class="actions">
        <button class="primary" onclick={restart}>Try again</button>
        <button onclick={copyCmd}>Copy command</button>
      </div>
    {:else}
      <span class="text">
				{#if release?.phase === 'partial'}
					<strong>Orbit updated with partial restoration.</strong>
					Some resources need attention. Inspect <code>orbit status --json</code> for exact recovery.
				{:else if release?.phase === 'failed'}
					<strong>Orbit couldn’t finish the update.</strong>
					{release.last_error ?? 'The current installation remains available.'}
				{:else if release?.target_version}
					<strong>Orbit <code>{release.target_version}</code> is verified and ready.</strong>
					{release.defer_reason === 'resources_running'
						? 'It will update automatically after all product resources stop.'
						: 'Orbit will apply it at the next safe command boundary.'}
				{:else}
					<strong>Orbit <code>{targetToken}</code> is ready.</strong>
					The daemon is still running <code>{runningToken}</code>. Restart Orbit to apply it; running resources will be restored.
				{/if}
        <details>
          <summary>Build details</summary>
					{#if release?.target_version}<span>Released: <code>{release.target_version}</code></span>{/if}
					<span>Installed: <code>{version?.on_disk ?? version?.running ?? 'unknown'}</code></span>
					<span>Running: <code>{version?.running ?? 'unknown'}</code></span>
					{#if version?.on_disk_path}<span>Binary: <code>{version.on_disk_path}</code></span>{/if}
        </details>
      </span>
      <div class="actions">
				{#if release?.phase !== 'partial' && release?.phase !== 'failed'}
					<button class="primary" onclick={packageManaged ? copyCmd : restart}>{packageManaged ? 'Copy upgrade command' : release?.target_version ? 'Update now' : 'Restart now'}</button>
				{/if}
        <button onclick={dismiss}>Later</button>
      </div>
    {/if}
  </div>
{/if}

<style>
  .banner {
    display: flex;
    align-items: center;
    gap: 0.6rem; /* off-grid: intentional compact banner rhythm */
    padding: 0.35rem var(--space-4); /* 0.35rem off-grid vertical */
    background: rgba(210, 153, 34, 0.1);
    color: var(--yellow);
    border-bottom: 1px solid rgba(210, 153, 34, 0.25);
    font-size: var(--text-md);
  }
  .banner.success {
    background: color-mix(in srgb, var(--green) 10%, transparent);
    color: var(--green);
    border-bottom-color: color-mix(in srgb, var(--green) 25%, transparent);
  }
  .banner.error {
    background: color-mix(in srgb, var(--red) 10%, transparent);
    color: var(--red);
    border-bottom-color: color-mix(in srgb, var(--red) 25%, transparent);
  }
  .text { flex: 1; color: var(--dim); }
  .text strong { color: var(--yellow); }
  .text :global(code) {
    color: var(--yellow);
    background: color-mix(in srgb, currentColor 12%, transparent);
    padding: 0.05rem 0.3rem; /* off-grid: intentional tight inline code padding */
    border-radius: 3px; /* off-grid: between 0 and radius-sm */
    font-family: ui-monospace, monospace;
    font-size: var(--text-md);
  }
  .success .text strong, .success .text :global(code) { color: var(--green); }
  .error .text strong, .error .text :global(code) { color: var(--red); }
  details { display: inline; margin-left: var(--space-2); }
  summary { display: inline; cursor: pointer; color: var(--dim); }
  details span { display: block; margin-top: var(--space-1); }
  .actions { display: flex; gap: var(--space-2); }
  button {
    background: transparent;
    color: var(--dim);
    border: 1px solid var(--border);
    padding: 0.15rem 0.55rem; /* off-grid: intentional compact banner button */
    border-radius: 3px; /* off-grid: between 0 and radius-sm */
    cursor: pointer;
    font-size: var(--text-md);
  }
  button:hover { color: var(--fg); border-color: var(--dim); }
  button.primary { color: var(--yellow); border-color: currentColor; }
  .error button.primary { color: var(--red); }
  button:disabled { cursor: default; opacity: 0.7; }
</style>
