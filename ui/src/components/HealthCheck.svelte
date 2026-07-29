<script lang="ts">
  import { store } from '$lib/stores.svelte'
  import { runDoctorChecks } from '$lib/doctor'
  import { copyToClipboard } from '$lib/clipboard'
  import { Check, ChevronDown, ChevronRight, Copy, Info, Minus, TriangleAlert, X } from '@lucide/svelte'

  const statusLabels: Record<string, string> = {
    pass: 'passed',
    fail: 'failed',
    warn: 'warning',
    info: 'info',
  }

  let showOtherChecks = $state(false)
  const attentionChecks = $derived(
    store.daemon.doctorChecks.filter((check) => check.status === 'fail' || check.status === 'warn'),
  )
  const otherChecks = $derived(
    store.daemon.doctorChecks.filter((check) => check.status !== 'fail' && check.status !== 'warn'),
  )

  function commandFromHint(hint?: string): string {
    return hint?.startsWith('run: ') ? hint.slice('run: '.length).trim() : ''
  }
</script>

<div class="doctor-section">
  <div class="doctor-toolbar">
    <button
      disabled={store.daemon.doctorRunning}
      aria-busy={store.daemon.doctorRunning}
      onclick={() => runDoctorChecks()}
    >
      {store.daemon.doctorRunning ? 'Running...' : 'Run Checks'}
    </button>
    {#if store.daemon.doctorRanAt}
      <span class="doctor-timestamp">Last: {store.daemon.doctorRanAt}</span>
    {/if}
    {#if store.daemon.doctorChecks.length}
      <span class="doctor-summary">
        {#if store.daemon.doctorPassCount}<span class="pass">{store.daemon.doctorPassCount} pass</span>{/if}
        {#if store.daemon.doctorFailCount}<span class="fail">{store.daemon.doctorFailCount} fail</span>{/if}
        {#if store.daemon.doctorWarnCount}<span class="warn">{store.daemon.doctorWarnCount} warn</span>{/if}
      </span>
    {/if}
  </div>

  {#if store.daemon.doctorChecks.length}
    <div
      class:ready={attentionChecks.length === 0}
      class:attention={attentionChecks.length > 0}
      class="doctor-result"
      role="status"
    >
      {#if attentionChecks.length}
        <strong>{attentionChecks.length} {attentionChecks.length === 1 ? 'issue needs' : 'issues need'} attention</strong>
        <span>Follow the remedy below, then run the checks again.</span>
      {:else}
        <strong>Environment is ready</strong>
        <span>Orbit found no setup problems.</span>
      {/if}
    </div>

    {#each attentionChecks as c (c.name)}
      <div class="doctor-check">
        <span
          class="doctor-icon {c.status}"
          role="status"
          aria-label={statusLabels[c.status] ?? c.status}
        >
          {#if c.status === 'pass'}
            <Check size={14} strokeWidth={2.4} />
          {:else if c.status === 'fail'}
            <X size={14} strokeWidth={2.4} />
          {:else if c.status === 'warn'}
            <TriangleAlert size={14} strokeWidth={2.2} />
          {:else if c.status === 'info'}
            <Info size={14} strokeWidth={2.2} />
          {:else}
            <Minus size={14} strokeWidth={2.2} />
          {/if}
        </span>
        <span class="doctor-text">
          <span class="name">{c.name}</span>
          <span class="msg">{c.message}</span>
          {#if commandFromHint(c.hint)}
            <span class="doctor-remedy">
              <span class="remedy-label">Run this in your terminal</span>
              <span class="command-row">
                <code>{commandFromHint(c.hint)}</code>
                <button
                  type="button"
                  class="copy-command"
                  aria-label="Copy remedy command for {c.name}"
                  onclick={() => copyToClipboard(commandFromHint(c.hint), 'Remedy command copied')}
                >
                  <Copy size={13} strokeWidth={2.2} />
                  Copy
                </button>
              </span>
            </span>
          {:else if c.hint}
            <span class="doctor-remedy">
              <span class="remedy-label">How to fix it</span>
              <span class="hint">{c.hint}</span>
            </span>
          {/if}
        </span>
      </div>
    {/each}

    {#if otherChecks.length}
      <button
        type="button"
        class="other-checks-toggle"
        aria-expanded={showOtherChecks}
        onclick={() => { showOtherChecks = !showOtherChecks }}
      >
        {#if showOtherChecks}
          <ChevronDown size={14} />
          Hide {otherChecks.length} other {otherChecks.length === 1 ? 'check' : 'checks'}
        {:else}
          <ChevronRight size={14} />
          Show {otherChecks.length} other {otherChecks.length === 1 ? 'check' : 'checks'}
        {/if}
      </button>
    {/if}

    {#if showOtherChecks}
      {#each otherChecks as c (c.name)}
        <div class="doctor-check doctor-check-secondary">
          <span
            class="doctor-icon {c.status}"
            role="status"
            aria-label={statusLabels[c.status] ?? c.status}
          >
            {#if c.status === 'pass'}
              <Check size={14} strokeWidth={2.4} />
            {:else if c.status === 'info'}
              <Info size={14} strokeWidth={2.2} />
            {:else}
              <Minus size={14} strokeWidth={2.2} />
            {/if}
          </span>
          <span class="doctor-text">
            <span class="name">{c.name}</span>
            <span class="msg">{c.message}</span>
          </span>
        </div>
      {/each}
    {/if}
  {:else if !store.daemon.doctorRunning}
    <div class="doctor-empty">No results yet.</div>
  {/if}
</div>

<style>
  .doctor-section {
    margin: 0 var(--space-5) var(--space-4);
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    overflow: hidden;
  }
  .doctor-toolbar {
    padding: 0.6rem var(--space-4); /* 0.6rem off-grid vertical */
    display: flex;
    align-items: center;
    gap: var(--space-4);
    border-bottom: 1px solid var(--border);
  }
  .doctor-timestamp {
    font-size: var(--text-sm);
    color: var(--dim);
  }
  .doctor-check {
    display: grid;
    grid-template-columns: 2rem 1fr;
    align-items: baseline;
    gap: 0.3rem; /* off-grid: intentional tight grid gutter */
    padding: 0.4rem var(--space-4); /* 0.4rem off-grid vertical */
    border-top: 1px solid var(--border);
  }
  .doctor-check:first-child {
    border-top: none;
  }
  .doctor-check-secondary {
    background: color-mix(in srgb, var(--card) 88%, var(--bg));
  }
  .doctor-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-weight: 700;
    font-size: var(--text-md);
  }
  .doctor-icon.pass { color: var(--green); }
  .doctor-icon.fail { color: var(--red); }
  .doctor-icon.warn { color: var(--yellow); }
  .doctor-icon.info { color: var(--dim); }
  .doctor-text {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    font-size: var(--text-md);
  }
  .doctor-text .name {
    font-weight: 600;
  }
  .doctor-text .msg {
    color: var(--dim);
    margin-left: var(--space-2);
  }
  .doctor-result {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border-bottom: 1px solid var(--border);
  }
  .doctor-result strong {
    font-size: var(--text-base);
  }
  .doctor-result span {
    color: var(--dim);
    font-size: var(--text-md);
  }
  .doctor-result.ready strong {
    color: var(--green);
  }
  .doctor-result.attention strong {
    color: var(--yellow);
  }
  .doctor-remedy {
    flex: 1 0 100%;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin-top: var(--space-2);
  }
  .remedy-label {
    color: var(--dim);
    font-size: var(--text-sm);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }
  .command-row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .command-row code {
    min-width: 0;
    overflow-wrap: anywhere;
    color: var(--fg);
    font-size: var(--text-md);
  }
  .copy-command {
    flex: 0 0 auto;
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    min-height: 28px;
    padding: var(--space-1) var(--space-2);
  }
  .hint {
    color: var(--fg);
  }
  .other-checks-toggle {
    width: 100%;
    display: flex;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-2) var(--space-4);
    border: 0;
    border-top: 1px solid var(--border);
    border-radius: 0;
    background: transparent;
    color: var(--dim);
    text-align: left;
  }
  .other-checks-toggle:hover {
    color: var(--fg);
    background: color-mix(in srgb, var(--card) 75%, var(--border));
  }
  .doctor-empty {
    padding: var(--space-3) var(--space-4);
    color: var(--dim);
    font-size: var(--text-md);
  }
  .doctor-summary {
    margin-left: auto;
    display: flex;
    gap: var(--space-2);
    font-size: var(--text-sm);
  }
  .doctor-summary .pass { color: var(--green); }
  .doctor-summary .fail { color: var(--red); }
  .doctor-summary .warn { color: var(--yellow); }
</style>
