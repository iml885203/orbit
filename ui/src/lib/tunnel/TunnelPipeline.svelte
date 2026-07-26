<script lang="ts">
  import { copyToClipboard } from '$lib/clipboard'
  import TunnelAccessLog from './TunnelAccessLog.svelte'
  import type { TunnelView } from './api'

  let { tunnel }: { tunnel: TunnelView } = $props()

  const releaseAllCmd = $derived(`orbit tunnel release --to ${tunnel.local_port}`)

  // Map backend status → a flow state that drives the pipeline's colour + motion.
  // healthy = flowing green; connecting/reconnecting = pulsing blue (in-flight);
  // error = static red. Anything unknown falls back to the dim "idle" look.
  const flow = $derived(
    tunnel.status === 'healthy'
      ? 'healthy'
      : tunnel.status === 'connecting' || tunnel.status === 'reconnecting'
        ? 'pending'
        : tunnel.status === 'error'
          ? 'error'
          : 'idle',
  )

  const statusLabel = $derived(tunnel.status === 'reconnecting' ? 'reconnecting' : tunnel.status)
</script>

<article class="tunnel-row" data-flow={flow} aria-label="Tunnel for local port {tunnel.local_port}">
  <header class="row">
    <div class="endpoint origin">
      <span class="node-dot"></span>
      <div class="node-text">
        <span class="node-label">staging</span>
        <span class="node-sub">tunlease</span>
      </div>
    </div>

    <div class="wire">
      <div class="wire-track"></div>
      <div class="pulse"></div>
      <span class="hop">Tunlease</span>
    </div>

    <div class="endpoint dest">
      <div class="node-text">
        <span class="node-label">:{tunnel.local_port}</span>
        <span class="node-sub">your machine</span>
      </div>
      <span class="node-dot"></span>
    </div>

    <span class="status-pill" data-flow={flow}>{statusLabel}</span>
  </header>

  <div class="meta">
    <ul class="paths">
      {#each tunnel.paths as p (p)}
        <li>
          <button
            class="path"
            title="Copy: orbit tunnel release {p}"
            aria-label="Copy release command for {p}"
            onclick={() => copyToClipboard(`orbit tunnel release ${p}`, 'Copied release command')}
          >
            {p}
          </button>
        </li>
      {/each}
    </ul>
    {#if tunnel.proxy_port > 0}
      <span class="proxy">proxy :{tunnel.proxy_port}</span>
    {/if}
  </div>

  {#if tunnel.status === 'error' && tunnel.last_error}
    <p class="err" role="alert">{tunnel.last_error}</p>
  {/if}

  <div class="actions">
    <button
      class="cmd"
      title="Copy to clipboard"
      aria-label="Copy command: {releaseAllCmd}"
      onclick={() => copyToClipboard(releaseAllCmd, 'Copied release command')}
    >
      <span class="cmd-prompt">$</span>{releaseAllCmd}
      <span class="cmd-copy" aria-hidden="true">⧉</span>
    </button>
    <span class="hint-text">click a path to copy its release command</span>
  </div>

  <TunnelAccessLog localPort={tunnel.local_port} />
</article>

<style>
  .tunnel-row {
    /* Fill the (capped) list width instead of growing to the grid's intrinsic
       content width — without this the .row grid below expands the card to the
       full viewport, overflowing its <li> (the "full-bleed" bug). */
    width: 100%;
    box-sizing: border-box;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    padding: var(--space-4) var(--space-5);
    /* Left rail tinted by flow state — the at-a-glance health cue. */
    border-left: 3px solid var(--rail);
  }
  .tunnel-row[data-flow='healthy'] { --rail: var(--green); --accent: var(--green); }
  .tunnel-row[data-flow='pending'] { --rail: var(--blue);  --accent: var(--blue); }
  .tunnel-row[data-flow='error']   { --rail: var(--red);   --accent: var(--red); }
  .tunnel-row[data-flow='idle']    { --rail: var(--dim);   --accent: var(--dim); }

  .row {
    display: grid;
    grid-template-columns: auto 1fr auto auto;
    align-items: center;
    gap: var(--space-4);
    min-width: 0; /* let the 1fr wire column shrink instead of forcing overflow */
  }

  .endpoint { display: flex; align-items: center; gap: var(--space-3); }
  .dest { justify-content: flex-end; }
  .node-text { display: flex; flex-direction: column; line-height: 1.2; }
  .dest .node-text { text-align: right; }
  .node-label {
    font-family: var(--font-mono);
    font-size: var(--text-base);
    color: var(--fg);
  }
  .node-sub {
    font-size: var(--text-xs);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--dim);
  }
  .node-dot {
    width: 9px; height: 9px;
    border-radius: var(--radius-pill);
    background: var(--accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 18%, transparent);
    flex: none;
  }

  /* The wire: a track + a travelling pulse + the gateway hop label centred on it. */
  .wire { position: relative; height: 28px; display: flex; align-items: center; }
  .wire-track {
    position: absolute; inset-inline: 0; top: 50%;
    height: 2px; transform: translateY(-50%);
    background: linear-gradient(
      90deg,
      color-mix(in srgb, var(--accent) 50%, transparent),
      color-mix(in srgb, var(--accent) 12%, transparent)
    );
  }
  .pulse {
    position: absolute; top: 50%; left: 0;
    width: 6px; height: 6px; border-radius: var(--radius-pill);
    transform: translateY(-50%);
    background: var(--accent);
    box-shadow: 0 0 8px 1px var(--accent);
    opacity: 0;
  }
  .hop {
    position: relative;
    margin: 0 auto;
    padding: 2px var(--space-3);
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-pill);
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--dim);
    z-index: 1;
  }

  /* healthy: a pulse glides origin→dest on a slow loop (a "breathing" stand-in for
     real traffic — see access-log backlog). pending: the same but faster + the rail
     hint shimmers. error/idle: no motion. */
  .tunnel-row[data-flow='healthy'] .pulse {
    animation: travel 3.2s linear infinite;
  }
  .tunnel-row[data-flow='pending'] .pulse {
    animation: travel 1.1s linear infinite;
  }
  @keyframes travel {
    0%   { left: 0;    opacity: 0; }
    12%  { opacity: 1; }
    88%  { opacity: 1; }
    100% { left: 100%; opacity: 0; }
  }
  @media (prefers-reduced-motion: reduce) {
    .pulse { animation: none !important; }
  }

  .status-pill {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 2px var(--space-3);
    border-radius: var(--radius-pill);
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 14%, transparent);
    white-space: nowrap;
  }

  .meta {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-4);
    margin-top: var(--space-3);
    padding-top: var(--space-3);
    border-top: 1px dashed var(--border);
  }
  .paths { list-style: none; display: flex; flex-wrap: wrap; gap: var(--space-2); }
  .path {
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    color: var(--fg);
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 1px var(--space-2);
    cursor: pointer;
    transition: border-color var(--transition-fast), color var(--transition-fast);
  }
  .path:hover { border-color: var(--accent); color: var(--accent); }
  .proxy {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--dim);
    white-space: nowrap;
  }
  .err {
    margin-top: var(--space-3);
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    color: var(--red);
    word-break: break-word;
  }

  /* Release command: a copyable terminal-style chip + a hint about per-path copy. */
  .actions {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin-top: var(--space-3);
  }
  .cmd {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    color: var(--fg);
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: var(--space-1) var(--space-3);
    cursor: pointer;
    transition: border-color var(--transition-fast);
  }
  .cmd:hover { border-color: var(--blue); }
  .cmd-prompt { color: var(--dim); }
  .cmd-copy { color: var(--dim); font-size: var(--text-md); }
  .cmd:hover .cmd-copy { color: var(--blue); }
  .hint-text {
    font-size: var(--text-xs);
    color: var(--dim);
  }
</style>
