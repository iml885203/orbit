<script lang="ts">
  import { tunnelAccess } from './tunnelAccess.svelte'

  let { localPort }: { localPort: number } = $props()
  let open = $state(false)

  const lines = $derived(tunnelAccess.byPort[localPort] ?? [])

  function hhmmss(iso: string): string {
    const d = new Date(iso)
    return d.toTimeString().slice(0, 8)
  }
  function statusClass(s: number): string {
    if (s >= 500) return 'err'
    if (s >= 400) return 'warn'
    return 'ok'
  }
</script>

<div class="access">
  <button class="toggle" aria-expanded={open} onclick={() => (open = !open)}>
    {open ? '▾' : '▸'} access log ({lines.length})
  </button>
  {#if open}
    {#if lines.length === 0}
      <p class="empty">waiting for callbacks…</p>
    {:else}
      <ul class="log" role="log">
        {#each lines as l (l.time + l.method + l.path + l.status + l.duration_ms)}
          <li>
            <span class="t">{hhmmss(l.time)}</span>
            <span class="m">{l.method}</span>
            <span class="p">{l.path}</span>
            <span class="s {statusClass(l.status)}">{l.status}</span>
            <span class="d">{l.duration_ms}ms</span>
          </li>
        {/each}
      </ul>
    {/if}
  {/if}
</div>

<style>
  .access { margin-top: var(--space-3); }
  .toggle {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--dim);
    background: none;
    border: none;
    cursor: pointer;
    padding: 0;
  }
  .toggle:hover { color: var(--fg); }
  .empty { font-size: var(--text-xs); color: var(--dim); margin: var(--space-2) 0 0; }
  .log {
    list-style: none;
    margin: var(--space-2) 0 0;
    max-height: 180px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .log li {
    display: grid;
    grid-template-columns: auto auto 1fr auto auto;
    gap: var(--space-3);
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--fg);
    padding: 1px var(--space-2);
  }
  .t { color: var(--dim); }
  .m { color: var(--blue); }
  .p { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .s.ok { color: var(--green); }
  .s.warn { color: var(--yellow); }
  .s.err { color: var(--red); }
  .d { color: var(--dim); }
</style>
