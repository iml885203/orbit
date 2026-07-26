<script lang="ts">
  import { Cable, Loader2 } from '@lucide/svelte'
  import { claimTunnel } from './api'

  let { onClaimed }: { onClaimed: () => void | Promise<void> } = $props()

  let localPort = $state('')
  let callbackPath = $state('')
  let busy = $state(false)
  let error = $state('')

  async function submit() {
    if (busy) return
    const port = Number(localPort)
    const path = callbackPath.trim()
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      error = 'Enter a local port from 1 to 65535.'
      return
    }
    if (!path.startsWith('/')) {
      error = 'Callback path must start with /.'
      return
    }
    busy = true
    error = ''
    try {
      const result = await claimTunnel(port, path)
      if (!result.ok) {
        error = result.data?.error || 'Could not create the tunnel.'
        return
      }
      callbackPath = ''
      await onClaimed()
    } finally {
      busy = false
    }
  }
</script>

<form class="claim-form" aria-label="Create tunnel" aria-busy={busy} onsubmit={(event) => { event.preventDefault(); void submit() }}>
  <div class="intro">
    <strong>Route a callback</strong>
    <span>Forward a staging callback to a port on this machine.</span>
  </div>
  <label>
    <span>Local port</span>
    <input type="number" min="1" max="65535" inputmode="numeric" placeholder="8080" bind:value={localPort} disabled={busy} />
  </label>
  <label class="path">
    <span>Callback path</span>
    <input type="text" placeholder="/callbacks/provider/getbalance" bind:value={callbackPath} disabled={busy} />
  </label>
  <button class="primary" type="submit" disabled={busy} aria-busy={busy}>
    {#if busy}<Loader2 size={14} class="spin" aria-hidden="true" /> Creating…{:else}<Cable size={14} aria-hidden="true" /> Create tunnel{/if}
  </button>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
</form>

<style>
  .claim-form {
    display: grid;
    grid-template-columns: minmax(180px, 1fr) 120px minmax(240px, 2fr) auto;
    align-items: end;
    gap: var(--space-3);
    max-width: 900px;
    margin: var(--space-3) var(--space-5);
    padding: var(--space-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    background: var(--card);
  }
  .intro { display: flex; flex-direction: column; gap: var(--space-1); align-self: center; }
  .intro strong { font-size: var(--text-base); }
  .intro span, label span { color: var(--dim); font-size: var(--text-sm); }
  label { display: flex; flex-direction: column; gap: var(--space-1); }
  input { width: 100%; }
  button { display: inline-flex; align-items: center; justify-content: center; gap: var(--space-1); white-space: nowrap; }
  .error { grid-column: 2 / -1; color: var(--red); font-size: var(--text-sm); margin: 0; }
  :global(.spin) { animation: spin 1s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  @media (prefers-reduced-motion: reduce) { :global(.spin) { animation: none; } }
  @media (max-width: 760px) {
    .claim-form { grid-template-columns: 1fr; }
    .error { grid-column: auto; }
  }
</style>
