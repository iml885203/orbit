<script lang="ts">
  import { tooltip } from '$lib/tooltip.svelte'
  import { ScrollText, GitCompareArrows, Loader2 } from '@lucide/svelte'
  type Props = { running: boolean; total: number; elapsedSeconds: number; disabledReason?: string; hasLog: boolean; checkingAll?: boolean; checkProgress?: { done: number; total: number }; onPublishAll: () => void; onCheckAll: () => void; onViewLog: () => void }
  let { running, total, elapsedSeconds, disabledReason = '', hasLog, checkingAll = false, checkProgress, onPublishAll, onCheckAll, onViewLog }: Props = $props()
  const scope = $derived(`${total} database${total === 1 ? '' : 's'}`)
  const checkLabel = $derived(checkProgress && checkProgress.total > 0 ? `Checking ${checkProgress.done}/${checkProgress.total}…` : 'Checking…')
</script>
<header class="page-header">
  <div><h1>Database Projects</h1><p>Check schema changes, publish safely, or reset local data.</p>{#if running}<div class="progress" role="status">Publishing {scope} · {elapsedSeconds}s</div>{:else}<p class="scope">{scope}</p>{/if}</div>
  <div class="actions">{#if hasLog}<button type="button" onclick={onViewLog}><ScrollText size={14} /> View log</button>{/if}<button type="button" disabled={checkingAll || total === 0 || !!disabledReason} aria-busy={checkingAll} use:tooltip={{ content: disabledReason || `Refreshes schema status for every database` }} onclick={onCheckAll}>{#if checkingAll}<Loader2 size={14} class="spin" aria-hidden="true" /> {checkLabel}{:else}<GitCompareArrows size={14} aria-hidden="true" /> Refresh all{/if}</button><button class="primary" type="button" disabled={!!disabledReason || total === 0} aria-busy={running} use:tooltip={{ content: disabledReason || `Publishes ${scope}; stops at the first failure` }} onclick={onPublishAll}>{running ? `Publishing ${total}…` : 'Publish all'}</button></div>
</header>
<style>
  .page-header { display: flex; align-items: center; justify-content: space-between; gap: var(--space-5); }
  h1 { margin: 0; font-size: var(--text-2xl); } p { margin: 4px 0 0; color: var(--dim); }
  .actions { display: flex; gap: var(--space-2); } button { display: inline-flex; align-items: center; gap: var(--space-1); }
  .progress { margin-top: var(--space-2); color: var(--blue); font-size: var(--text-sm); font-weight: 600; }
  .scope { margin-top: var(--space-1); color: var(--dim); font-size: var(--text-sm); }
  @media (max-width: 650px) { .page-header { align-items: flex-start; flex-direction: column; } }
  :global(.spin) { animation: spin 1s linear infinite; }
  @media (prefers-reduced-motion: reduce) { :global(.spin) { animation: none; } }
  @keyframes spin { to { transform: rotate(360deg); } }
</style>
