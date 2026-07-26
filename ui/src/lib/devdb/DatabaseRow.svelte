<script lang="ts">
  import type { DBState, DBResetState } from '$lib/types.gen'
  import type { DBOpInFlight } from './stores.svelte'
  import { Loader2, ScrollText, TriangleAlert, GitCompareArrows, Check } from '@lucide/svelte'
  import type { DriftSummary } from './drift.svelte'

  type Props = { database: string; state?: DBState; resetState?: DBResetState; operation?: DBOpInFlight | null; disabledReason?: string; submittingVerb?: 'publish' | 'reset' | null; compact?: boolean; elapsedSeconds?: number; drift?: DriftSummary; diffing?: boolean; onPublish: () => void; onReset: () => void; onViewLog: () => void; onDiff?: () => void; onForcePublish?: () => void }
  let { database, state, resetState, operation, disabledReason = '', submittingVerb = null, compact = false, elapsedSeconds = 0, drift, diffing = false, onPublish, onReset, onViewLog, onDiff, onForcePublish }: Props = $props()
  // Drift badge (on-demand): shown only after a diff has run. In sync → ok;
  // changes → warn; changes with possible data loss → err; a failed diff →
  // muted error; a stale summary → muted "recheck". Colour never carries
  // meaning alone — the label text always states it.
  const driftView = $derived(
    !drift ? null
    : drift.error ? { pill: 'pill-muted', label: 'Check failed', icon: 'alert' as const }
    : drift.needsEngine ? { pill: 'pill-muted', label: 'Check required', icon: null }
    : drift.stale ? { pill: 'pill-muted', label: drift.inSync ? 'Recheck' : `${drift.changes} change${drift.changes === 1 ? '' : 's'} · recheck`, icon: null }
    : drift.inSync ? { pill: 'pill-ok', label: 'In sync', icon: 'check' as const }
    : drift.dataLoss ? { pill: 'pill-err', label: `${drift.changes} change${drift.changes === 1 ? '' : 's'} · data loss`, icon: 'alert' as const }
    : { pill: 'pill-warn', label: `${drift.changes} change${drift.changes === 1 ? '' : 's'}`, icon: null },
  )
  const driftTitle = $derived(
    drift?.error ? `Schema check failed: ${drift.error}`
    : drift?.needsEngine ? 'Check this database once to start tracking schema changes'
    : drift?.stale ? 'Schema status is refreshing'
    : 'Current schema status',
  )
  const running = $derived(!!operation && !operation.done && (operation.db === database || !!operation.all))
  const showLog = $derived(!!operation && (running || operation.db === database || !!operation.all))
  const disabled = $derived(!!disabledReason)
  // A DB known not to exist can't be reset because publish creates it.
  // Undefined means unknown, so the server remains authoritative.
  const notExists = $derived(resetState?.exists === false)
  const resetDisabled = $derived(disabled || notExists)
  const resetTitle = $derived(notExists ? 'Publish this database before resetting it' : disabledReason)
  const diffLabel = $derived(
    diffing ? 'Checking…'
    : drift?.needsEngine ? 'Check'
    : drift && !drift.inSync ? 'View changes'
    : 'Check',
  )
  const publishNeedsConfirmation = $derived(!!drift?.dataLoss && !drift.stale && !drift.error && !drift.needsEngine)
  // Only a single-db publish paints a row Failed: an --all run stops at
  // the first failure, so earlier DBs succeeded and later ones never ran —
  // per-row Failed would lie. The --all failure shows as a list banner.
  const failed = $derived(!!operation?.done && !operation?.ok && !operation.all && operation?.db === database)
  // A publish blocked on possible data loss gets its own next action: the
  // user reviews the row's failure text (the log has the exact drops) and
  // can force the publish here instead of switching to the CLI's --force.
  const dataLossBlocked = $derived(failed && operation?.op === 'publish' && operation?.errorCode === 'publish_blocked_data_loss' && !!onForcePublish)
  const status = $derived(
    running ? { pill: 'pill-info', dot: true, label: operation?.op === 'reset' ? 'Resetting' : 'Publishing' }
    : failed ? { pill: 'pill-err', dot: false, label: operation?.op === 'reset' ? 'Reset failed' : 'Publish failed' }
    : notExists ? { pill: 'pill-muted', dot: false, label: 'Not published' }
    : state?.lastPublish || state?.lastReset ? { pill: 'pill-ok', dot: false, label: 'Published' }
    : { pill: 'pill-muted', dot: false, label: 'Status unknown' },
  )
  const lastEvent = $derived(state?.lastReset && (!state.lastPublish || Date.parse(state.lastReset.at) > Date.parse(state.lastPublish.at))
    ? { label: 'Last reset', event: state.lastReset }
    : state?.lastPublish ? { label: 'Last publish', event: state.lastPublish } : null)

  function relative(at: string): string {
    const seconds = Math.max(0, Math.floor((Date.now() - new Date(at).getTime()) / 1000))
    if (seconds < 60) return `${seconds}s ago`
    const minutes = Math.floor(seconds / 60)
    if (minutes < 60) return `${minutes}m ago`
    const hours = Math.floor(minutes / 60)
    return hours < 24 ? `${hours}h ago` : `${Math.floor(hours / 24)}d ago`
  }
</script>

<article class="database-row" class:compact class:running>
  <div class="head">
    <div class="identity"><strong>{database}</strong><span class="pill {status.pill}" role="status">{#if status.dot}<span class="dot" aria-hidden="true"></span>{/if}{status.label}</span>{#if driftView}<span class="pill {driftView.pill}" class:stale={drift?.stale} role="status" title={driftTitle}>{#if driftView.icon === 'check'}<Check size={11} aria-hidden="true" />{:else if driftView.icon === 'alert'}<TriangleAlert size={11} aria-hidden="true" />{/if}{driftView.label}</span>{/if}</div>
    <div class="history">
      {#if lastEvent}<span><small>{lastEvent.label}</small>{relative(lastEvent.event.at)}{lastEvent.event.source ? ` · ${lastEvent.event.source}` : ''}{lastEvent.event.durationMs ? ` · ${(lastEvent.event.durationMs / 1000).toFixed(1)}s` : ''}</span>{:else}<span class="not-published"><small>History</small>No tracked operations</span>{/if}
    </div>
  </div>
  {#if running}<div class="running-label"><Loader2 size={14} class="spin" aria-hidden="true" />{operation?.op === 'reset' ? 'Resetting' : 'Publishing'}… {elapsedSeconds}s</div>
  {:else if dataLossBlocked}<div class="failure" role="alert">Publish was blocked: it would discard data (see the log for exactly what). If the change is intentional, publish anyway.<button class="danger inline" type="button" disabled={disabled} onclick={onForcePublish}><TriangleAlert size={13} aria-hidden="true" /> Publish anyway…</button></div>
  {:else if failed}<div class="failure" role="alert">{operation?.err ?? `${operation?.op === 'reset' ? 'Reset' : 'Publish'} could not complete.`} View the log for details.</div>{/if}
  <div class="action-line">
    <span class="helper">{notExists ? 'Publish creates this database.' : 'Apply schema changes and keep local data.'}</span>
    {#if disabledReason}<span class="disabled-reason">{disabledReason}</span>{/if}
    <div class="actions">
    <button class="primary" type="button" disabled={disabled} aria-busy={(running && operation?.op === 'publish') || submittingVerb === 'publish'} title={disabledReason || (publishNeedsConfirmation ? 'Review the data-loss warning before publishing' : '')} onclick={onPublish}>{submittingVerb === 'publish' ? 'Starting…' : publishNeedsConfirmation ? 'Publish…' : 'Publish'}</button>
    <button class="danger" type="button" disabled={resetDisabled} aria-busy={(running && operation?.op === 'reset') || submittingVerb === 'reset'} title={resetTitle} onclick={onReset}><TriangleAlert size={13} aria-hidden="true" /> {submittingVerb === 'reset' ? 'Starting…' : 'Reset…'}</button>
    {#if onDiff}<button type="button" disabled={disabled || diffing} aria-busy={diffing} aria-label={`Check schema changes for ${database}`} title="View schema changes" onclick={onDiff}>{#if diffing}<Loader2 size={14} class="spin" aria-hidden="true" />{:else}<GitCompareArrows size={14} aria-hidden="true" />{/if}{diffLabel}</button>{/if}
    {#if showLog}<button class="icon" type="button" aria-label={`View ${database} operation log`} title="View log" onclick={onViewLog}><ScrollText size={14} aria-hidden="true" /></button>{/if}
    </div>
  </div>
</article>

<style>
  .database-row { display: flex; flex-direction: column; gap: var(--space-3); padding: var(--space-4); border: 1px solid var(--border); border-radius: var(--radius-lg); background: var(--card); }
  .database-row.running { border-color: color-mix(in srgb, var(--blue) 45%, var(--border)); background: color-mix(in srgb, var(--blue) 5%, var(--card)); }
  .head { display: flex; justify-content: space-between; align-items: baseline; gap: var(--space-4); flex-wrap: wrap; }
  .identity { display: flex; align-items: center; gap: var(--space-2); min-width: 0; }
  .identity strong { overflow: hidden; text-overflow: ellipsis; font-family: var(--font-mono); }
  .history { display: flex; gap: var(--space-4); min-width: 0; flex-wrap: wrap; color: var(--text-secondary); font-size: var(--text-sm); }
  .history span { display: flex; flex-direction: column; }
  .history small { color: var(--dim); text-transform: uppercase; font-size: var(--text-xs); font-weight: 700; }
  .not-published { color: var(--dim); }
  .running-label { color: var(--blue); font-size: var(--text-sm); display: flex; align-items: center; gap: var(--space-1); }
  .failure { color: var(--red); font-size: var(--text-sm); }
  .failure button.inline { margin-left: var(--space-2); vertical-align: middle; }
  .action-line { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); flex-wrap: wrap; }
  .helper, .disabled-reason { color: var(--dim); font-size: var(--text-sm); }
  .disabled-reason { color: var(--text-secondary); }
  .actions { display: flex; justify-content: flex-end; gap: var(--space-2); }
  button { display: inline-flex; align-items: center; gap: var(--space-1); }
  button.icon { padding-inline: var(--space-2); }
  .pill { gap: 5px; }
  .pill.stale { border: 1px dashed color-mix(in srgb, var(--fg) 30%, transparent); opacity: 0.75; }
  .dot { width: 7px; height: 7px; border-radius: 50%; background: currentColor; animation: pulse 1.2s ease-in-out infinite; }
  @keyframes pulse { 0%,100% { opacity: 1; } 50% { opacity: .35; } }
  @media (prefers-reduced-motion: reduce) { .dot { animation: none; } }
  @media (max-width: 560px) { .actions { flex-wrap: wrap; } .actions .primary { flex: 1 1 auto; justify-content: center; } }
  .compact { padding: var(--space-3) 0; border-width: 1px 0 0; border-radius: 0; background: transparent; }
  .compact .action-line { justify-content: flex-start; }
  :global(.spin) { animation: spin 1s linear infinite; }
  @media (prefers-reduced-motion: reduce) { :global(.spin) { animation: none; } }
  @keyframes spin { to { transform: rotate(360deg); } }
</style>
