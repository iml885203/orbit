<script lang="ts">
  import { fetchGlobalClaims, type GlobalClaim } from './api'

  // Collapsible gateway claim panel. Fetches on every expand — the
  // claim table changes as other devs come and go, so a stale cached view would
  // mislead. We don't poll while open / on page load, only on the explicit
  // expand, keeping gateway traffic to deliberate lookups.
  let open = $state(false)
  let loading = $state(false)
  let error = $state('')
  let claims = $state<GlobalClaim[]>([])
  let loadedOnce = $state(false)

  // ISO → local HH:MM (matches the access-log time rendering style).
  function hhmm(iso: string): string {
    const d = new Date(iso)
    return isNaN(d.getTime()) ? iso : d.toTimeString().slice(0, 5)
  }

  async function load() {
    loading = true
    error = ''
    try {
      claims = await fetchGlobalClaims()
      loadedOnce = true
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  function toggle() {
    open = !open
    if (open && !loading) load() // refetch every expand — the claim table is shared and changes
  }
</script>

<section class="global">
  <button
    class="toggle"
    aria-expanded={open}
    aria-busy={loading}
    onclick={toggle}
  >
    <span class="chev" class:open aria-hidden="true">▸</span>
    All claims on Tunlease
    {#if loadedOnce && !error}<span class="count">{claims.length}</span>{/if}
  </button>

  {#if open}
    <div class="body">
      {#if loading}
        <p class="msg" role="status">Loading…</p>
      {:else if error}
        <p class="msg err" role="status">
          {error}
          <button class="retry" onclick={load}>retry</button>
        </p>
      {:else if claims.length === 0}
        <p class="msg" role="status">No active claims on Tunlease.</p>
      {:else}
        <ul class="rows">
          {#each claims as c (c.path_prefix)}
            <li class="row" class:mine={c.mine}>
              <span class="path">{c.path_prefix}</span>
              <span class="owner">{c.owner}</span>
              <span class="exp">expires {hhmm(c.expires_at)}</span>
              {#if c.mine}<span class="you">you</span>{/if}
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  {/if}
</section>

<style>
  .global {
    max-width: 900px;
    margin: var(--space-4) var(--space-5) 0;
  }
  .toggle {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    background: none;
    border: none;
    cursor: pointer;
    color: var(--dim);
    font-size: var(--text-sm);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    font-weight: 600;
    padding: 0;
  }
  .toggle:hover { color: var(--fg); }
  .chev {
    transition: transform 0.12s ease;
    display: inline-block;
  }
  .chev.open { transform: rotate(90deg); }
  .count {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--fg);
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-pill);
    padding: 0 var(--space-2);
  }
  .body { margin-top: var(--space-2); }
  .msg {
    font-size: var(--text-sm);
    color: var(--dim);
    margin: var(--space-2) 0;
  }
  .msg.err { color: var(--red); }
  .retry {
    margin-left: var(--space-2);
    background: none;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--fg);
    cursor: pointer;
    font-size: var(--text-xs);
    padding: 0 var(--space-2);
  }
  .rows {
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin: 0;
    padding: 0;
  }
  .row {
    display: grid;
    grid-template-columns: minmax(0, 2fr) minmax(0, 2fr) auto auto;
    align-items: center;
    gap: var(--space-3);
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-sm);
  }
  .row.mine { background: color-mix(in srgb, var(--blue) 10%, transparent); }
  .path { color: var(--fg); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .owner { color: var(--dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .exp { color: var(--dim); font-size: var(--text-xs); }
  .you {
    color: var(--blue);
    font-size: var(--text-xs);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-weight: 600;
  }
</style>
