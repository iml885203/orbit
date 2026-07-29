<script lang="ts">
  import { onMount } from 'svelte'
  import { push, replace, router } from 'svelte-spa-router'
  import TraceTable from '../components/tracing/TraceTable.svelte'
  import TraceDetailModal from '../components/tracing/TraceDetailModal.svelte'
  import { tracing, fetchTraces, fetchTracingStatus } from '$lib/tracing.svelte'
  import { parseTraceQuery, buildTraceQuery } from '$lib/traceQuery'
  import { store } from '$lib/stores.svelte'
  import { copyToClipboard } from '$lib/clipboard'

  // svelte-spa-router maps both /tracing and /tracing/:traceId to this page
  // (same component instance, so opening the detail modal never remounts the
  // list — filters and scroll survive). params.traceId drives the modal.
  let { params }: { params?: { traceId?: string } } = $props()
  const detailId = $derived(params?.traceId ?? '')
  // Whether the modal was opened by an in-app row click (vs a direct deep
  // link). Governs the close path so Back returns to the list we came from,
  // and a deep-linked open still lands on the list instead of leaving Orbit.
  let openedFromList = $state(false)

  function openDetail(id: string) {
    openedFromList = true
    push('/tracing/' + id)
  }

  function closeDetail() {
    if (openedFromList) {
      openedFromList = false
      window.history.back() // restores /tracing + its filter query and scroll
    } else {
      const qs = buildTraceQuery({ errored, minDurationMs, search })
      replace('/tracing' + (qs ? '?' + qs : ''))
    }
  }

  let errored = $state(false)
  let minDurationMs = $state(0)
  let search = $state('')
  let statusTimer: ReturnType<typeof setInterval> | undefined
  // True until the first status + trace fetch settle, so the page shows a
  // loading state instead of flashing an empty/healthy view it hasn't
  // confirmed yet.
  let loading = $state(true)
  // Ticks so the Live/Idle freshness derivation re-evaluates on the same 3s
  // cadence as the status poll, without depending on wall-clock in $derived.
  let nowMs = $state(Date.now())

  // Initialise filters from the URL query so a shared/reloaded link restores
  // the same view, then mirror changes back into the query.
  function readQuery(qs: string) {
    const f = parseTraceQuery(qs)
    errored = f.errored
    minDurationMs = f.minDurationMs
    search = f.search
  }

  function syncQuery() {
    const qs = buildTraceQuery({ errored, minDurationMs, search })
    replace('/tracing' + (qs ? '?' + qs : ''))
  }

  onMount(() => {
    readQuery(router.querystring ?? '')
    // Settle loading once both the first status and the first trace fetch
    // return, so the confirmed state (healthy / down / empty) replaces the
    // loading view in one step rather than flickering through intermediates.
    const first = Promise.all([
      fetchTracingStatus().then((s) => (tracing.status = s)),
      fetchTraces(200).then((list) => {
        // Seed the store without clobbering live events that already landed.
        for (const t of list) tracing.upsert(t)
      }),
    ])
    first.finally(() => (loading = false))
    statusTimer = setInterval(() => {
      fetchTracingStatus().then((s) => (tracing.status = s))
      nowMs = Date.now()
    }, 3000)
    return () => {
      clearInterval(statusTimer)
    }
  })

  const filtered = $derived(
    tracing.traces.filter((t) => {
      if (errored && t.status !== 'error') return false
      if (minDurationMs > 0 && t.durationMs < minDurationMs) return false
      if (search.trim()) {
        const q = search.trim().toLowerCase()
        const hay = `${t.rootService} ${t.rootName} ${t.traceId} ${t.services.join(' ')}`.toLowerCase()
        if (!hay.includes(q)) return false
      }
      return true
    }),
  )

  // Healthy dev services with a URL — shown in the empty state so "generate
  // some traffic" comes with something concrete to hit.
  const trafficTargets = $derived(
    Object.values(store.daemon.services)
      .filter((s) => s.kind === 'service' && s.state === 'healthy' && s.url)
      .slice(0, 4),
  )

  // How recently a span must have arrived for the indicator to read "Live"
  // rather than "Idle". Comfortably longer than the 3s poll so a steady trickle
  // stays Live between ticks.
  const LIVE_WINDOW_MS = 15000

  const st = $derived(tracing.status)
  // Turned off for this env (explicit enabled: false).
  const disabled = $derived(st !== null && !st.configured)
  // On, but the OTLP receiver never bound (e.g. port conflict with no free
  // fallback) — distinct from "on and idle", which is a healthy empty state.
  const receiverDown = $derived(st !== null && st.configured && !st.receiverHealthy)
  // Fresh span within the window → Live; healthy but quiet → Idle.
  const live = $derived(
    st !== null && st.receiverHealthy && st.lastReceivedUnixMs > 0 && nowMs - st.lastReceivedUnixMs < LIVE_WINDOW_MS,
  )
</script>

<section class="tracing-page" aria-label="Tracing" inert={detailId !== ''}>
  <div class="toolbar">
    <h2 class="title">Tracing</h2>
    {#if tracing.statusUnavailable}
      <span class="live err" role="status">
        <span class="dot err"></span>
        Status unavailable
      </span>
    {:else if st}
      <span class="live" class:on={live} class:err={receiverDown} role="status">
        <span class="dot" class:on={live} class:err={receiverDown}></span>
        {#if disabled}Off
        {:else if receiverDown}Receiver down
        {:else if live}Live · {st.spansPerMin} spans/min
        {:else}Idle{/if}
      </span>
    {/if}
    <span class="spacer"></span>
    <button type="button" class="chip" class:active={errored} aria-pressed={errored}
      onclick={() => { errored = !errored; syncQuery() }}>Errors</button>
    <label class="chip num">
      ≥
      <input type="number" min="0" step="50" bind:value={minDurationMs}
        oninput={syncQuery} aria-label="Minimum duration in milliseconds" /> ms
    </label>
    <input class="search" type="search" placeholder="Search root, route, service, id"
      bind:value={search} oninput={syncQuery} aria-label="Search traces" />
  </div>

  {#if loading}
    <div class="empty" role="status" aria-busy="true">
      <p>Loading traces…</p>
    </div>
  {:else if disabled}
    <div class="empty">
      <p>Tracing is turned off for this env.</p>
      <p class="hint">Tracing is on by default. Remove <code>enabled: false</code> from the <code>tracing:</code> block in the env YAML, then <code>orbit down &amp;&amp; orbit up</code> to re-enable.</p>
    </div>
  {:else if receiverDown}
    <div class="empty">
      <p>The trace receiver did not start.</p>
      <p class="hint">Tracing is on, but the OTLP receiver could not bind port {st?.otlpPort ?? 4318}{#if st?.receiverError} — {st.receiverError}{/if}.</p>
      <p class="hint">Free the port or set a different <code>tracing.otlp_port</code>, then <code>orbit down &amp;&amp; orbit up</code>.</p>
    </div>
  {:else if tracing.traces.length === 0}
    <div class="empty">
      <p>No spans captured yet.</p>
      <p class="hint">Receiver ready on port {st?.actualPort ?? st?.otlpPort ?? 4318}. No instrumented service has sent a span yet.</p>
      {#if trafficTargets.length > 0}
        <p class="hint">After adding OpenTelemetry instrumentation, generate traffic against a running service:</p>
        <ul class="targets">
          {#each trafficTargets as svc (svc.name)}
            <li>
              <span class="tname">{svc.name}</span>
              <code>curl -sk {svc.url}</code>
              <button type="button" class="copy" aria-label={`Copy curl for ${svc.name}`}
                onclick={() => copyToClipboard(`curl -sk ${svc.url}`, 'curl copied')}>📋</button>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="hint">No healthy services are running.</p>
        <button type="button" class="link" onclick={() => push('/')}>Open Services</button>
      {/if}
    </div>
  {:else if filtered.length === 0}
    <div class="empty">
      <p>No traces match the current filters.</p>
      <button type="button" class="link" onclick={() => { errored = false; minDurationMs = 0; search = ''; syncQuery() }}>Clear filters</button>
    </div>
  {:else}
    <TraceTable traces={filtered} onOpen={openDetail} />
  {/if}
</section>

{#if detailId}
  <TraceDetailModal traceId={detailId} onClose={closeDetail} />
{/if}

<style>
  .tracing-page { display: flex; flex: 1 1 auto; flex-direction: column; min-height: 0; padding: var(--space-4) var(--space-5); }
  .toolbar { display: flex; align-items: center; gap: var(--space-3); margin-bottom: var(--space-3); flex-wrap: wrap; }
  .title { font-size: var(--text-lg); font-weight: 600; }
  .spacer { flex: 1; }
  .live { display: inline-flex; align-items: center; gap: var(--space-2); font-size: var(--text-sm); color: var(--dim); }
  .live.on { color: var(--green); }
  .live.err { color: var(--red); }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--dim); }
  .dot.on { background: var(--green); box-shadow: 0 0 8px color-mix(in srgb, var(--green) 60%, transparent); }
  .dot.err { background: var(--red); }
  .chip {
    background: color-mix(in srgb, var(--card) 50%, transparent);
    border: 1px solid var(--border);
    color: var(--dim);
    border-radius: var(--radius-md);
    padding: var(--space-1) var(--space-3);
    font-size: var(--text-md);
    font-family: inherit;
    cursor: pointer;
    min-height: var(--hit-target);
  }
  .chip.active { color: var(--red); border-color: color-mix(in srgb, var(--red) 55%, var(--border)); background: color-mix(in srgb, var(--red) 8%, transparent); }
  .chip.num { display: inline-flex; align-items: center; gap: var(--space-1); cursor: default; }
  .chip.num input { width: 64px; background: var(--bg); border: 1px solid var(--border); color: var(--fg); border-radius: var(--radius-sm); padding: 2px 4px; font-family: var(--font-mono); }
  .search {
    background: var(--bg); border: 1px solid var(--border); color: var(--fg);
    border-radius: var(--radius-md); padding: var(--space-1) var(--space-3);
    font-size: var(--text-md); min-width: 220px; min-height: var(--hit-target);
  }
  .empty { color: var(--fg); padding: var(--space-6) var(--space-4); text-align: center; }
  .empty .hint { color: var(--dim); font-size: var(--text-md); margin-top: var(--space-2); }
  .targets {
    list-style: none;
    padding: 0;
    margin: var(--space-3) auto 0;
    display: inline-flex;
    flex-direction: column;
    gap: var(--space-2);
    text-align: left;
  }
  .targets li { display: flex; align-items: center; gap: var(--space-2); }
  .targets .tname { font-family: var(--font-mono); color: var(--dim); min-width: 90px; }
  .targets .copy {
    background: none; border: 1px solid var(--border); border-radius: var(--radius-sm);
    cursor: pointer; padding: 1px 6px; font-size: var(--text-sm);
  }
  .targets .copy:hover { border-color: var(--blue); }
  .link { background: none; border: 0; color: var(--blue); cursor: pointer; font-size: var(--text-md); }
  .link:hover { text-decoration: underline; }
  code { font-family: var(--font-mono); background: color-mix(in srgb, var(--fg) 10%, transparent); padding: 1px 5px; border-radius: var(--radius-sm); }
</style>
