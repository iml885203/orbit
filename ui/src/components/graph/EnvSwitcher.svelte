<script lang="ts">
  import { store, toast, replaceGraphData, openLogViewer } from '../../lib/stores.svelte'
  import { opProgress } from '../../lib/opProgress.svelte'
  import { switchEnv, apiPost, fetchGraph } from '../../lib/api'
  import { environmentActionState } from '../../lib/graphActions'
  import { Power, PowerOff, Database, ExternalLink, ScrollText } from '@lucide/svelte'
  import ConfirmModal from '../ConfirmModal.svelte'

  const context = $derived(store.daemon.envs?.context)
  const current = $derived(context?.display_name || store.graph.data?.env || '')
  // Count live state, not the cached running value from /api/envs (which
  // only refreshes on initial mount / SSE reconnect — would be stale
  // right after the user just started something).
  const running = $derived(
    Object.values(store.daemon.services).filter(
      s => s.state !== 'stopped' && s.state !== 'pending'
    ).length
  )
  const actionState = $derived(environmentActionState(store.graph.data?.nodes ?? []))
  const primaryAction = $derived(actionState.primary)
  const instanceContext = $derived(store.daemon.instanceName ? ` in instance \`${store.daemon.instanceName}\`` : '')

  type PendingAction =
    | { kind: 'switch'; env: string; current: string; currentIdentity: string; targetIdentity: string; resources: string[] }
    | { kind: 'down' }
  let pending = $state<PendingAction | null>(null)

  const confirmConfig = $derived.by(() => {
    if (!pending) return null
    if (pending.kind === 'switch') {
      return {
        title: 'Switch environment?',
        message: `Switching to ${pending.env} will stop ${pending.resources.length} running item${pending.resources.length === 1 ? '' : 's'} from ${pending.current}${instanceContext}: ${pending.resources.join(', ')}. You'll need to run \`orbit up\` afterwards.`,
        confirmLabel: 'Stop and switch',
      }
    }
    return {
      title: 'Stop everything?',
      message: `This will stop ${running} running service${running === 1 ? '' : 's'} and container${running === 1 ? '' : 's'}${instanceContext}.`,
      confirmLabel: 'Stop all',
    }
  })

  async function doSwitch(short: string, confirmed = false, currentIdentity = '', targetIdentity = '', confirmedResources: string[] = []) {
    const total = running
    store.graph.envSwitching = { target: short, total, phase: 'stopping' }
    const { ok, data } = await switchEnv(short, confirmed, currentIdentity, targetIdentity, confirmedResources)
    if (!ok) {
      if (data?.confirmation_required && data.current_context) {
        pending = {
          kind: 'switch',
          env: data.target_context?.display_name || short,
          current: data.current_context.display_name,
          currentIdentity: data.current_context.identity,
          targetIdentity: data.target_context?.identity || '',
          resources: data.running_resources ?? [],
        }
        store.graph.envSwitching = null
        return
      }
      toast(data?.error || 'Failed to switch env')
      store.graph.envSwitching = null
      return
    }
    toast(data?.message || `Switched to ${short}`)
    // We're now live on this env; clear any preview so the watermark
    // and read-only state come off immediately rather than waiting for
    // the SSE tick.
    store.graph.preview = null
    // Stop phase done; daemon's reloaded cfg, but our store.graph.data
    // still holds the previous env until the next SSE tick brings the
    // new graph in. Flip to "loading" phase and wait for that to land
    // before clearing the overlay.
    store.graph.envSwitching = { target: short, total, phase: 'loading' }
    if (!(await waitForGraphEnv(short, 10_000))) {
      // Don't silently clear a wait that never completed: show a stalled
      // phase (daemon may still be restarting) and keep watching. Only
      // after the extended window give up loudly.
      store.graph.envSwitching = { target: short, total, phase: 'stalled' }
      if (!(await waitForGraphEnv(short, 20_000))) {
        toast(`Switched to ${short}, but its graph never loaded — is the daemon back up? Try reloading.`)
      }
    }
    store.graph.envSwitching = null
  }

  // Promotion path from preview → live. Re-uses the same confirm modal
  // as the legacy "click tab = switch" flow; the difference is intent —
  // the user explicitly clicked "Use this env" rather than misclicking
  // a tab.
  function useThisEnv() {
    if (!store.graph.preview) return
    const target = store.graph.preview.env
    doSwitch(target)
  }

  // Wait for the live graph to reflect the target env, with a deadline.
  // Actively fetches /api/graph instead of watching store.graph.data:
  // the SSE-driven refetch in App.svelte is keyed on service status, and
  // two envs whose service snapshots happen to match produce the same key
  // — passively waiting on the store would stall forever in that case.
  // (A polling loop, not runed.watch, because this runs post-await in
  // doSwitch, outside Svelte's effect context — watch() throws
  // effect_orphan there.)
  // Resolves true when the live graph reflects the target env, false when
  // the timeout elapses first — callers decide how loudly to react.
  async function waitForGraphEnv(target: string, timeoutMs: number): Promise<boolean> {
    const deadline = Date.now() + timeoutMs
    while (Date.now() < deadline) {
      const g = await fetchGraph()
      if (g) replaceGraphData(g)
      if (g?.env === target) return true
      await new Promise(r => setTimeout(r, 500))
    }
    return false
  }

  async function doDown() {
    if (busy) return
    busy = true
    try {
      const { ok, data } = await apiPost('/api/down', { all: false })
      if (ok) opProgress.start('down')
      else toast(data?.error || 'Failed')
    } finally {
      busy = false
    }
  }

  function onConfirm() {
    if (!pending) return
    const p = pending
    pending = null
    if (p.kind === 'switch') doSwitch(p.env, true, p.currentIdentity, p.targetIdentity, p.resources)
    else if (p.kind === 'down') doDown()
  }

  function onCancel() {
    pending = null
  }

  async function upAll() {
    if (busy) return
    busy = true
    try {
      const { ok, data } = await apiPost('/api/up', {})
      if (ok) opProgress.start('up')
      else toast(data?.error || 'Failed')
    } finally {
      busy = false
    }
  }

  async function downAll() {
    if (busy) return
    if (running === 0) {
      toast('Nothing running')
      return
    }
    pending = { kind: 'down' }
  }

  async function infraOnly() {
    if (busy) return
    busy = true
    try {
      const { ok, data } = await apiPost('/api/up', { infra_only: true })
      if (ok) opProgress.start('infra')
      else toast(data?.error || 'Failed')
    } finally {
      busy = false
    }
  }

  let busy = $state(false)
  async function runPrimaryAction() {
    const action = primaryAction
    if (!action || action.kind === 'busy' || busy) return
    switch (action.kind) {
      case 'start':
        if (action.resource) {
          busy = true
          try {
            const { ok, data } = await apiPost('/api/up', { resources: [action.resource] })
            if (!ok) toast(data?.error || `Failed to start ${action.resource}`)
          } finally {
            busy = false
          }
        } else {
          await upAll()
        }
        break
      case 'logs':
        openLogViewer(action.resource)
        break
      case 'inspect':
        store.graph.selectedNode = action.resource
        break
      case 'open':
        window.open(action.url, '_blank')
        break
    }
  }
</script>

<nav class="env-bar" aria-label="Service controls">
  {#if store.graph.preview}
    <div class="preview-context"><span>Previewing</span><strong>{store.graph.preview.env}</strong></div>
  {:else}
    <div class="current-context">
      <span>Environment</span>
      <strong>{current}</strong>
      {#if context?.kind === 'project'}<span class="context-kind">Project environment</span>
      {:else if context?.kind === 'explicit'}<span class="context-kind">Explicit config</span>{/if}
      <span class="environment-summary" role="status">{actionState.summary}</span>
    </div>
  {/if}
  <div class="toolbar">
    {#if store.graph.preview}
      <button
        class="toolbar-btn primary"
        type="button"
        title={`Activate ${store.graph.preview.env} — stops services from ${current}`}
        onclick={useThisEnv}
      ><Power size={14} /> Use this env</button>
      <button
        class="toolbar-btn"
        type="button"
        title="Return to the live env view"
        onclick={() => store.graph.preview = null}
      >Exit preview</button>
    {:else}
      {#if primaryAction}
        <button
          class="toolbar-btn primary"
          type="button"
          disabled={primaryAction.kind === 'busy' || busy}
          aria-busy={busy}
          onclick={runPrimaryAction}
        >
          {#if primaryAction.kind === 'open'}<ExternalLink size={14} />
          {:else if primaryAction.kind === 'logs' || primaryAction.kind === 'inspect'}<ScrollText size={14} />
          {:else}<Power size={14} />{/if}
          {busy ? 'Working…' : primaryAction.label}
        </button>
      {/if}
      {#if running > 0}
        <button class="toolbar-btn danger" type="button" title="Stop the current environment" disabled={busy} aria-busy={busy} onclick={downAll}><PowerOff size={14} /> Stop environment</button>
      {/if}
      <details class="advanced-actions">
        <summary>More</summary>
        <div class="advanced-menu">
          <button type="button" disabled={busy} aria-busy={busy} onclick={infraOnly}><Database size={14} /> Start infrastructure only</button>
        </div>
      </details>
    {/if}
  </div>
</nav>

{#if confirmConfig}
  <ConfirmModal
    open={true}
    title={confirmConfig.title}
    message={confirmConfig.message}
    confirmLabel={confirmConfig.confirmLabel}
    cancelLabel="Cancel"
    danger
    {onConfirm}
    {onCancel}
  />
{/if}

<style>
  .env-bar {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: 52px;
    padding: var(--space-2) var(--space-3);
    border-bottom: 1px solid var(--border);
    background:
      linear-gradient(180deg, rgba(88, 166, 255, 0.05), transparent 85%),
      rgba(13, 17, 23, 0.78);
  }

  .current-context, .preview-context {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--dim);
    font-size: var(--text-sm);
  }
  .current-context strong, .preview-context strong {
    color: var(--fg);
    font-family: var(--font-mono);
    font-size: var(--text-md);
  }
  .environment-summary {
    padding-left: var(--space-2);
    border-left: 1px solid var(--border);
    color: var(--dim);
  }
  .context-kind {
    color: var(--blue);
    font-size: var(--text-xs);
  }
  .preview-context {
    color: var(--blue);
  }

  .toolbar-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .toolbar-btn.primary:disabled {
    background: var(--card);
    border-color: var(--border);
    color: var(--dim);
  }
  .toolbar-btn.primary:disabled:hover {
    background: var(--card);
    border-color: var(--border);
    color: var(--dim);
  }

  .toolbar {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin-left: auto;
  }

  .toolbar-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    background: transparent;
    border: 1px solid var(--dim);
    color: var(--dim);
    padding: var(--space-2) var(--space-3);
    cursor: pointer;
    font-family: var(--font-mono);
    font-size: var(--text-md);
    border-radius: var(--radius-md);
    transition: color var(--transition-fast), border-color var(--transition-fast), background var(--transition-fast);
  }
  .toolbar-btn:hover {
    color: var(--fg);
    border-color: var(--fg);
    background: rgba(255, 255, 255, 0.05);
  }
  .toolbar-btn.danger:hover {
    color: var(--red);
    border-color: var(--red);
  }
  .toolbar-btn.primary {
    background: var(--blue);
    border-color: var(--blue);
    color: var(--white);
  }
  .toolbar-btn.primary:hover {
    background: color-mix(in srgb, var(--blue) 85%, white);
    border-color: var(--blue);
    color: var(--white);
  }

  .advanced-actions {
    position: relative;
  }
  .advanced-actions summary {
    min-height: 32px;
    display: inline-flex;
    align-items: center;
    padding: var(--space-2) var(--space-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--dim);
    cursor: pointer;
    font-family: var(--font-mono);
    font-size: var(--text-md);
    list-style: none;
  }
  .advanced-actions summary::-webkit-details-marker {
    display: none;
  }
  .advanced-actions summary:hover {
    color: var(--fg);
    border-color: var(--dim);
  }
  .advanced-menu {
    position: absolute;
    top: calc(100% + var(--space-1));
    right: 0;
    z-index: 50;
    min-width: 220px;
    padding: var(--space-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--card);
  }
  .advanced-menu button {
    width: 100%;
    display: flex;
    align-items: center;
    gap: var(--space-2);
    border: 0;
    text-align: left;
    white-space: nowrap;
  }

  @media (max-width: 640px) {
    .env-bar {
      align-items: flex-start;
      flex-direction: column;
    }
    .toolbar {
      width: 100%;
      margin-left: 0;
      flex-wrap: wrap;
    }
    .toolbar-btn.primary {
      flex: 1 1 auto;
      justify-content: center;
    }
  }
</style>
