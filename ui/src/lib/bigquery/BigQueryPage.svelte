<script lang="ts">
  import { onMount } from 'svelte'
  import Btn from '$components/Btn.svelte'
  import { bqState, refreshBigQuery } from './state.svelte'

  onMount(() => {
    void refreshBigQuery()
    const timer = setInterval(() => void refreshBigQuery(), 5000)
    return () => clearInterval(timer)
  })
  const snapshot = $derived(bqState.snapshot)
</script>

<div class="bq-page">
  <header>
    <h2 class="page-title">BigQuery</h2>
    <Btn label="Refresh" onclick={() => void refreshBigQuery()} />
  </header>
  {#if !bqState.reachable}
    <p class="notice" role="alert">
      {snapshot ? 'Connection lost. Showing the last local snapshot.' : 'Could not load BigQuery configuration. Retry when the daemon is reachable.'}
    </p>
  {/if}
  {#if !snapshot && bqState.reachable}
    <p role="status" aria-busy="true">Loading BigQuery configuration…</p>
  {:else if snapshot && !snapshot.configured}
    <p role="status">The active environment has no BigQuery configuration.</p>
  {:else if snapshot}
    <p class="provenance">Local recovery evidence · refreshed {new Date(snapshot.observed_at).toLocaleTimeString()}</p>
    <p class="explanation">Cloud access, GoogleSQL compatibility and application readiness are unverified. Run cloud operations from the CLI with your ADC identity.</p>
    {#if snapshot.error}<p class="notice" role="alert">{snapshot.error}</p>{/if}
    <section aria-labelledby="bq-profiles">
      <h3 id="bq-profiles">Profiles</h3>
      <div class="profiles">
        {#each snapshot.profiles as profile (profile.name)}
          <article>
            <h4>{profile.name} <span>{profile.mode}</span></h4>
            <dl>
              <dt>Job project</dt><dd>{profile.job_project}</dd>
              <dt>Data project</dt><dd>{profile.data_project}</dd>
              <dt>Location</dt><dd>{profile.location}</dd>
              {#if profile.dataset}<dt>Dataset</dt><dd>{profile.dataset}</dd>{/if}
            </dl>
          </article>
        {/each}
      </div>
    </section>
    <section aria-labelledby="bq-sandboxes">
      <h3 id="bq-sandboxes">Sandbox records</h3>
      {#if snapshot.error}
        <p class="notice" role="status">Sandbox records are unavailable. Preserve local evidence and inspect it with the CLI before preparing another run.</p>
      {:else if snapshot.records.length === 0}
        <p class="empty">No local sandbox records. Prepare a sandbox with <code>orbit bq sandbox prepare PROFILE --run RUN_KEY --json</code>.</p>
      {:else}
        <div class="records">
          {#each snapshot.records as record (record.scope)}
            <article>
              <h4>{record.profile_name} <span>{record.phase}</span></h4>
              <dl>
                <dt>Run key</dt><dd>{record.run_key}</dd>
                <dt>Dataset</dt><dd>{record.target.data_project}.{record.dataset_id}</dd>
                <dt>Data</dt><dd>{record.sandbox_data_ready ? 'Ready at last CLI observation' : 'Not ready / cleanup in progress'}</dd>
                <dt>Observed</dt><dd>{record.observed_at || 'No successful observation'}</dd>
                <dt>App runtime</dt><dd>Unverified</dd>
                {#if record.last_error}<dt>Last error</dt><dd>{record.last_error}</dd>{/if}
              </dl>
              {#if record.warnings?.length}<p class="notice">Warnings: {record.warnings.join(', ')}</p>{/if}
              <details><summary>Recovery identity</summary><p class="identity">Instance: {record.instance || 'default'} · generation: {record.runtime_generation}</p></details>
            </article>
          {/each}
        </div>
      {/if}
    </section>
    <p class="explanation">Stopping Orbit does not delete cloud datasets. Cleanup requires an explicit CLI command after stopping the application and its jobs. Old instance generations retain evidence and require manual disposition.</p>
  {/if}
</div>

<style>
  .bq-page { padding: var(--space-5); display: flex; flex-direction: column; gap: var(--space-4); overflow: auto; height: 100%; }
  header { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); }
  section { display: flex; flex-direction: column; gap: var(--space-3); }
  h3 { font-size: var(--text-lg); }
  h4 { font-size: var(--text-base); overflow-wrap: anywhere; }
  h4 span { color: var(--dim); font-size: var(--text-md); font-weight: 400; margin-left: var(--space-2); }
  .profiles, .records { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 330px), 1fr)); gap: var(--space-3); }
  article { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius-md); padding: var(--space-4); min-width: 0; }
  dl { display: grid; grid-template-columns: 90px minmax(0, 1fr); gap: var(--space-2) var(--space-3); margin-top: var(--space-3); font-size: var(--text-md); }
  dt, .provenance, .explanation, .empty { color: var(--dim); }
  dd, code, .identity { overflow-wrap: anywhere; }
  dd, code { font-family: var(--font-mono); }
  .notice { color: var(--yellow); }
  .explanation, .provenance, .empty, .identity, .notice { font-size: var(--text-base); line-height: 1.5; }
  details { margin-top: var(--space-3); font-size: var(--text-md); }
  summary { cursor: pointer; color: var(--blue); }
  .identity { margin-top: var(--space-2); }
  @media (max-width: 600px) { .bq-page { padding: var(--space-4); } }
</style>
