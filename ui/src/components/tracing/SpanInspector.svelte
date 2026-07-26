<script lang="ts">
  import type { Span } from '$lib/types.gen'
  import { formatDurationMs } from '$lib/traceTimeline'

  let { span }: { span: Span | null } = $props()

  const attrEntries = $derived(span?.attributes ? Object.entries(span.attributes) : [])
</script>

<aside class="inspector" aria-label="Span details">
  {#if !span}
    <p class="hint">Select a span to see its attributes. Its log lines highlight in the panel below.</p>
  {:else}
    <h3 class="name">{span.name}</h3>
    <div class="sub">
      <span class="svc">{span.service}</span>
      <span class="pill status-pill {span.status === 'error' ? 'pill-err' : 'pill-ok'}" role="status">
        {span.status === 'error' ? 'Error' : span.status}
      </span>
      <span class="dur">{formatDurationMs(span.durationMs)}</span>
    </div>
    {#if span.statusMsg}
      <p class="status-msg">{span.statusMsg}</p>
    {/if}

    <h4>Attributes</h4>
    {#if attrEntries.length === 0}
      <p class="hint">No attributes.</p>
    {:else}
      <dl class="kv">
        {#each attrEntries as [k, v] (k)}
          <dt>{k}</dt>
          <dd>{v}</dd>
        {/each}
      </dl>
    {/if}
  {/if}
</aside>

<style>
  .inspector {
    padding: var(--space-3);
    background: color-mix(in srgb, var(--card) 60%, var(--bg));
    border-left: 1px solid var(--border);
    overflow-y: auto;
    font-size: var(--text-md);
  }
  .hint { color: var(--dim); }
  .name { font-size: var(--text-lg); margin: 0 0 var(--space-2); word-break: break-word; }
  .sub { display: flex; align-items: center; gap: var(--space-2); margin-bottom: var(--space-2); }
  .svc { font-family: var(--font-mono); color: var(--dim); }
  .status-pill { text-transform: capitalize; }
  .dur { font-family: var(--font-mono); color: var(--dim); margin-left: auto; }
  .status-msg {
    color: var(--red);
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    background: color-mix(in srgb, var(--red) 8%, transparent);
    padding: var(--space-2);
    border-radius: var(--radius-sm);
  }
  h4 { font-size: var(--text-md); margin: var(--space-3) 0 var(--space-1); }
  .kv {
    display: grid;
    grid-template-columns: minmax(90px, auto) 1fr;
    gap: var(--space-1) var(--space-2);
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    margin: 0;
  }
  .kv dt { color: var(--dim); }
  .kv dd { margin: 0; word-break: break-word; }
</style>
