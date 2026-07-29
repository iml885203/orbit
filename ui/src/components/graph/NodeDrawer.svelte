<script lang="ts">
  import { nodeDrawerPanels } from '$ext'
  import type { GraphNode, ResourceStatus } from '../../lib/types.gen'
  import ServiceControls from '../ServiceControls.svelte'
  import NodeEnvPanel from './NodeEnvPanel.svelte'
  import NodeDepsPanel from './NodeDepsPanel.svelte'
  import { openLogViewer, store, toast, mutationsDisabled } from '../../lib/stores.svelte'
  import { ICONS, COLORS } from '../../lib/constants'
  import Badge from '../Badge.svelte'
  import { apiPost } from '../../lib/api'
  import { X, Play, Square, RotateCcw, ExternalLink, AppWindow, ScrollText, ChevronDown, Copy, Cog } from '@lucide/svelte'
  import { fly } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import { MediaQuery, SvelteSet } from 'svelte/reactivity'
  import { tooltip } from '../../lib/tooltip.svelte'
  import Icon from '@iconify/svelte'

  // Reactive: skip the slide animation if the user prefers reduced motion.
  const reducedMotionQuery = new MediaQuery('prefers-reduced-motion: reduce')
  const reducedMotion = $derived(reducedMotionQuery.current)

  let { node, onClose }: { node: GraphNode | null; onClose: () => void } = $props()
  const drawerInfraIcon = $derived(node && node.kind === 'infra' ? node.icon : null)
  // Preview mode: the node was rendered from another env's on-disk yaml,
  // so no live process exists. All mutating controls are disabled — the
  // drawer becomes a read-only inspector. Daemon doesn't need to enforce
  // anything because we don't fire any mutation API calls from here.
  const isPreviewing = $derived(store.graph.isPreviewing)
  // readOnly covers BOTH hover-preview and an active env marked
  // previewOnly. Use this for every disabled= attribute below; reserve
  // isPreviewing for UI affordances specific to the hover-preview overlay.
  const readOnly = $derived(mutationsDisabled())
  const activeGraph = $derived(store.graph.active)

  function rootBlocker(start: GraphNode): GraphNode | null {
    const graph = activeGraph
    if (!graph || !start.blockedBy) return null
    const seen = new SvelteSet([start.name])
    let name: string | undefined = start.blockedBy
    let blocker: GraphNode | undefined
    while (name && !seen.has(name)) {
      seen.add(name)
      blocker = graph.nodes.find(candidate => candidate.name === name)
      if (!blocker) return null
      name = blocker.blockedBy
    }
    return blocker ?? null
  }
  const blockedByRoot = $derived(node ? rootBlocker(node) : null)

  // Primary lifecycle CTA for this node. We pick a single dominant
  // action based on state so the drawer header has one obvious button
  // instead of duplicating the six-button strip on the node.
  type Cta = { label: string; icon: 'play' | 'restart' | 'stop' | null; tone: 'primary' | 'danger' | 'muted'; disabled: boolean; run?: () => Promise<void> }
  let busy = $state(false)
  async function withBusy(fn: () => Promise<{ ok: boolean; data?: any }>, failMsg: string) {
    if (!node || busy) return
    busy = true
    try {
      const { ok, data } = await fn()
      if (!ok) toast(data?.error || failMsg)
    } finally {
      busy = false
    }
  }
  const cta = $derived.by<Cta | null>(() => {
    if (!node) return null
    if (node.portConflict) {
      return { label: 'Port blocked', icon: null, tone: 'muted', disabled: true }
    }
    if (blockedByRoot) {
      const blocker = blockedByRoot
      if (blocker.state === 'degraded') {
        return {
          label: `Restart ${blocker.name}`,
          icon: 'restart',
          tone: 'primary',
          disabled: busy,
          run: () => withBusy(
            () => apiPost('/api/restart/' + blocker.name),
            `Failed to restart ${blocker.name}`,
          ),
        }
      }
      return {
        label: `Start ${blocker.name}`,
        icon: 'play',
        tone: 'primary',
        disabled: busy,
        run: () => withBusy(
          () => apiPost('/api/up', { resources: [blocker.name] }),
          `Failed to start ${blocker.name}`,
        ),
      }
    }
    const name = node.name
    switch (node.state) {
      case 'stopped':
      case 'pending':
        return {
          label: 'Start',
          icon: 'play',
          tone: 'primary',
          disabled: busy,
          run: () => withBusy(() => apiPost('/api/up', { resources: [name] }), 'Failed to start'),
        }
      case 'healthy':
      case 'degraded':
        return {
          label: 'Stop',
          icon: 'stop',
          tone: 'danger',
          disabled: busy,
          run: () => withBusy(() => apiPost('/api/stop/' + name), 'Failed to stop'),
        }
      case 'starting':
      case 'building':
        return { label: 'Starting…', icon: null, tone: 'muted', disabled: true }
      case 'stopping':
        return { label: 'Stopping…', icon: null, tone: 'muted', disabled: true }
      default:
        return null
    }
  })

  // First port entry for the inline port chip in the actions row.
  const firstPort = $derived<[string, number] | null>(
    node?.ports ? (Object.entries(node.ports)[0] ?? null) : null
  )

  // The "Service" card: mode toggle, timing chips, env toggles. Hide if empty.
  const hasModeToggle = $derived(!!node?.mode)
  const hasEnvToggles = $derived(
    !!node && store.daemon.envToggles.some(t => t.service === node.name)
  )
  const hasTiming = $derived(
    !!node && node.state !== 'stopped' && node.state !== 'pending' &&
    (!!node.health || node.kind !== 'infra')
    // ServiceControls owns timing render; approximate: if not stopped
    // and it's a service, assume timing will render.
  )
  const shouldShowServiceCard = $derived(
    !!node && node.kind !== 'infra' && (hasModeToggle || hasEnvToggles || hasTiming)
  )
  const serviceCardTitle = $derived(
    hasModeToggle && hasEnvToggles ? 'Mode & toggles' :
    hasEnvToggles ? 'Toggles' :
    hasModeToggle ? 'Mode' :
    'Runtime'
  )

  // Collapse state per card. Default everything open. Persists for the
  // life of this drawer instance only (resets when the user navigates).
  // String-keyed: extension drawer panels register their own card keys.
  let openCards = $state<Record<string, boolean>>({ service: true, environment: true, dependencies: true, kafka: true })
  function toggleCard(key: string) {
    // Uninitialized keys (extension panels) render open via `?? true` —
    // the toggle must honour that default or the first click no-ops.
    openCards[key] = !(openCards[key] ?? true)
  }

  const canRestart = $derived(
    !!node && !node.portConflict && !blockedByRoot &&
    ['healthy', 'degraded', 'building', 'starting'].includes(node.state)
  )
  function doRestart() {
    if (!node) return
    const name = node.name
    withBusy(() => apiPost('/api/restart/' + name), 'Failed to restart')
  }
  function openUrl() {
    if (node?.url) window.open(node.url, '_blank')
  }
  function openLogs() {
    if (node) openLogViewer(node.name)
  }
  function openSidecar(url: string) {
    window.open(url, '_blank')
  }
  async function copyPort(port: number) {
    try {
      await navigator.clipboard.writeText(String(port))
      toast(`Copied :${port}`)
    } catch {
      toast('Copy failed')
    }
  }
  async function copyPortInspection() {
    if (!node?.portConflict?.inspect_command) return
    try {
      await navigator.clipboard.writeText(node.portConflict.inspect_command)
      toast('Copied port inspection command')
    } catch {
      toast('Copy failed')
    }
  }

  function navigateTo(depName: string) {
    if (activeGraph?.nodes.find(n => n.name === depName)) store.graph.selectedNode = depName
  }

  // ServiceControls only consumes a strict subset of ResourceStatus. Define
  // that subset here so a future ResourceStatus field rename surfaces as a
  // type error at the adapter rather than a runtime mismatch.
  type ServiceControlsInput = Pick<ResourceStatus, 'name' | 'state' | 'mode' | 'url' | 'ports' | 'restart_count' | 'external_restart_count' | 'last_restart' | 'startup_time' | 'uptime' | 'health_progress' | 'kind'>

  // Adapt GraphNode → ResourceStatus shape ServiceControls expects.
  // GraphNode carries `health` where ResourceStatus has `health_progress`.
  const svc = $derived<ServiceControlsInput | null>(
    node
      ? {
          name: node.name,
          kind: (node.kind === 'infra' ? 'container' : 'service') as ResourceStatus['kind'],
          state: node.state,
          mode: node.mode,
          url: node.url,
          ports: node.ports,
          health_progress: node.health,
          restart_count: node.restart_count ?? 0,
          external_restart_count: node.external_restart_count ?? 0,
          last_restart: node.last_restart,
          startup_time: node.startup_time,
          uptime: node.uptime,
        }
      : null
  )

  // Derived: whether the deps panel has anything to show (drives card visibility).
  const hasDeps = $derived(
    !!(node && activeGraph && activeGraph.edges.some(e => e.from === node.name))
  )

  // Kafka card data: per-topic partner counts. `direction` labels what
  // the selected node does with the topic; we count partners on the
  // OPPOSITE side of other nodes in the graph.
  type KafkaTopicRef = { topic: string; count: number; partners: string[] }
  function computeKafkaList(
    self: string,
    topics: string[],
    direction: 'produces' | 'consumes',
  ): KafkaTopicRef[] {
    const graph = activeGraph
    if (!graph) return topics.map(t => ({ topic: t, count: 0, partners: [] }))
    return topics.map(topic => {
      const partners: string[] = []
      for (const n of graph.nodes) {
        if (n.name === self) continue
        const partnerSide = direction === 'produces' ? n.kafka?.consumes : n.kafka?.produces
        if (partnerSide?.includes(topic)) partners.push(n.name)
      }
      return { topic, count: partners.length, partners }
    })
  }
  const producesList = $derived<KafkaTopicRef[]>(
    computeKafkaList(node?.name ?? '', node?.kafka?.produces ?? [], 'produces'),
  )
  const consumesList = $derived<KafkaTopicRef[]>(
    computeKafkaList(node?.name ?? '', node?.kafka?.consumes ?? [], 'consumes'),
  )
  const hasKafka = $derived(producesList.length > 0 || consumesList.length > 0)

  function handleKey(e: KeyboardEvent) {
    if (e.key === 'Escape') onClose()
  }
</script>

<svelte:window onkeydown={handleKey} />

{#if node && svc}
  <div
    class="drawer"
    role="dialog"
    aria-labelledby="drawer-title"
    in:fly={{ x: reducedMotion ? 0 : 420, duration: reducedMotion ? 0 : 220, easing: cubicOut, opacity: reducedMotion ? 1 : 0 }}
    out:fly={{ x: reducedMotion ? 0 : 420, duration: reducedMotion ? 0 : 150, easing: cubicOut, opacity: reducedMotion ? 1 : 0 }}
  >
    <header>
      <div class="title">
        <span
          class="state-dot"
          style:color={COLORS[node.state]}
          aria-hidden="true"
        >{ICONS[node.state] ?? '?'}</span>
        {#if node.kind === 'infra'}
          <span class="infra-icon" data-testid="node-drawer-infra-icon" aria-hidden="true">
            {#if drawerInfraIcon}
              <Icon icon={drawerInfraIcon} width="24" height="24" />
            {:else}
              <Cog size={24} strokeWidth={2} />
            {/if}
          </span>
        {/if}
        <h2 id="drawer-title">{node.name}</h2>
        <Badge state={node.state} />
      </div>
      {#if node.stateReason && !node.portConflict}
        <p class="state-reason" role="status">{node.stateReason}</p>
      {/if}
      <div class="header-actions">
        {#if cta}
          <button
            class="cta tone-{cta.tone}"
            type="button"
            disabled={cta.disabled || readOnly}
            onclick={() => cta.run?.()}
          >
            {#if cta.icon === 'play'}<Play size={14} strokeWidth={2.25} />{/if}
            {#if cta.icon === 'restart'}<RotateCcw size={14} strokeWidth={2.25} />{/if}
            {#if cta.icon === 'stop'}<Square size={14} strokeWidth={2.25} fill="currentColor" />{/if}
            <span>{cta.label}</span>
          </button>
        {/if}
        <button class="close" type="button" aria-label="Close" onclick={onClose}><X size={18} /></button>
      </div>
    </header>
    <div class="body">
      {#if node.portConflict}
        <section class="port-conflict" role="alert" aria-label="Port conflict">
          <div>
            <strong>Port {node.portConflict.port} is already in use</strong>
            <p>Stop its owner or change {node.portConflict.resource}'s host port, then start the environment again.</p>
          </div>
          <button class="secondary-btn" type="button" onclick={copyPortInspection}>
            <Copy size={14} strokeWidth={2.25} />
            <span>Copy inspection command</span>
          </button>
        </section>
      {/if}
      {#if node.failureKind === 'health'}
        <section class="health-failure" role="status" aria-label="Health check failure">
          <strong>The process is still running</strong>
          <p>Orbit keeps checking it and will recover automatically after the health endpoint is fixed. Logs may explain the failure; restart only retries the process.</p>
        </section>
      {/if}
      <div class="secondary-actions">
        {#if canRestart}
          <button class="secondary-btn" type="button" disabled={busy || readOnly} onclick={doRestart}>
            <RotateCcw size={14} strokeWidth={2.25} />
            <span>Restart</span>
          </button>
        {/if}
        {#if node.sidecars}
          {#each node.sidecars as sc (sc.name)}
            <button class="secondary-btn" type="button" disabled={readOnly} onclick={() => openSidecar(sc.url)}>
              <AppWindow size={14} strokeWidth={2.25} />
              <span>{sc.name}</span>
            </button>
          {/each}
        {/if}
        {#if !node.portConflict || node.logsAvailable}
          <button class="secondary-btn" type="button" disabled={readOnly} onclick={openLogs}>
            <ScrollText size={14} strokeWidth={2.25} />
            <span>Logs</span>
          </button>
        {/if}
        {#if firstPort}
          <!-- Preview: URL is meaningless (no live process), so collapse to
               the copy-port path. The copy itself stays useful for
               inspection so we don't disable the chip outright. -->
          <button
            class="port-chip"
            type="button"
            onclick={isPreviewing
              ? () => copyPort(firstPort[1])
              : (node.url ? openUrl : () => copyPort(firstPort[1]))}
            use:tooltip={{ content: isPreviewing
              ? `Copy port ${firstPort[1]}`
              : (node.url ? node.url : `Copy port ${firstPort[1]}`) }}
          >
            {#if node.url && !isPreviewing}
              <ExternalLink size={12} strokeWidth={2.25} />
            {:else}
              <Copy size={12} strokeWidth={2.25} />
            {/if}
            <span>:{firstPort[1]}</span>
          </button>
        {/if}
      </div>

      {#if shouldShowServiceCard}
        <section class="card">
          <button
            class="card-title"
            type="button"
            aria-expanded={openCards.service}
            onclick={() => toggleCard('service')}
          >
            <ChevronDown size={14} class={openCards.service ? 'chev' : 'chev collapsed'} />
            <span>{serviceCardTitle}</span>
          </button>
          {#if openCards.service}
            <div class="card-body">
              <ServiceControls {svc} readOnly={readOnly} />
            </div>
          {/if}
        </section>
      {/if}

      {#if node.kind !== 'infra'}
        <section class="card">
          <button
            class="card-title"
            type="button"
            aria-expanded={openCards.environment}
            onclick={() => toggleCard('environment')}
          >
            <ChevronDown size={14} class={openCards.environment ? 'chev' : 'chev collapsed'} />
            <span>Environment</span>
          </button>
          {#if openCards.environment}
            <div class="card-body">
              <NodeEnvPanel {node} onNavigate={navigateTo} />
            </div>
          {/if}
        </section>
      {/if}

      {#if hasDeps}
        <section class="card">
          <button
            class="card-title"
            type="button"
            aria-expanded={openCards.dependencies}
            onclick={() => toggleCard('dependencies')}
          >
            <ChevronDown size={14} class={openCards.dependencies ? 'chev' : 'chev collapsed'} />
            <span>Dependencies</span>
          </button>
          {#if openCards.dependencies}
            <div class="card-body">
              <NodeDepsPanel {node} onNavigate={navigateTo} readOnly={readOnly} />
            </div>
          {/if}
        </section>
      {/if}

      {#if hasKafka}
        <section class="card">
          <button
            class="card-title"
            type="button"
            aria-expanded={openCards.kafka}
            onclick={() => toggleCard('kafka')}
          >
            <ChevronDown size={14} class={openCards.kafka ? 'chev' : 'chev collapsed'} />
            <span>Kafka</span>
          </button>
          {#if openCards.kafka}
            <div class="card-body">
              {#if producesList.length > 0}
                <div class="kafka-subhead">Produces</div>
                <ul class="kafka-topics">
                  {#each producesList as t (t.topic)}
                    <li>
                      <code>{t.topic}</code>
                      <span class="kafka-fan">→ {t.count} {t.count === 1 ? 'consumer' : 'consumers'}</span>
                    </li>
                  {/each}
                </ul>
              {/if}
              {#if consumesList.length > 0}
                <div class="kafka-subhead">Consumes</div>
                <ul class="kafka-topics">
                  {#each consumesList as t (t.topic)}
                    <li>
                      <code>{t.topic}</code>
                      <span class="kafka-fan">← {t.count} {t.count === 1 ? 'producer' : 'producers'}</span>
                    </li>
                  {/each}
                </ul>
              {/if}
            </div>
          {/if}
        </section>
      {/if}

      {#each nodeDrawerPanels.filter((p) => p.match(node.name)) as panel (panel.key)}
        <section class="card">
          <button
            class="card-title"
            type="button"
            aria-expanded={openCards[panel.key] ?? true}
            onclick={() => toggleCard(panel.key)}
          >
            <ChevronDown size={14} class={(openCards[panel.key] ?? true) ? 'chev' : 'chev collapsed'} />
            <span>{panel.title}</span>
          </button>
          {#if openCards[panel.key] ?? true}
            <div class="card-body">
              <panel.component />
            </div>
          {/if}
        </section>
      {/each}
    </div>
  </div>
{/if}

<style>
  .drawer {
    position: fixed; top: 60px; right: 0; bottom: 0;
    width: 420px;
    background: var(--card);
    border-left: 1px solid var(--border);
    overflow-y: auto;
    z-index: 50;
  }
  header {
    position: sticky;
    top: 0;
    z-index: 1;
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: var(--space-4);
    border-bottom: 1px solid var(--border);
    background: var(--card);
  }
  .title {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
  }
  .state-dot {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 14px;
    height: 14px;
    font-size: var(--text-md);
    line-height: 1;
    flex-shrink: 0;
  }
  .infra-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    color: var(--fg);
    flex-shrink: 0;
  }
  h2 { margin: 0; font-size: var(--text-xl); font-family: monospace; font-weight: 600; }
  .close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: 0;
    color: var(--dim);
    cursor: pointer;
    padding: var(--space-2);
    border-radius: var(--radius-sm);
    line-height: 0;
    transition: color var(--transition-fast), background var(--transition-fast);
  }
  .close:hover { color: var(--fg); background: rgba(255,255,255,0.06); }
  .header-actions {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex-shrink: 0;
  }
  .cta {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-1) var(--space-3);
    border-radius: var(--radius-sm);
    border: 1px solid transparent;
    font-size: var(--text-md);
    font-weight: 600;
    cursor: pointer;
    min-height: var(--hit-target);
    line-height: 1;
    transition: background var(--transition-fast), border-color var(--transition-fast), opacity var(--transition-fast), transform var(--transition-press);
  }
  .cta:active:not(:disabled) { transform: scale(0.96); }
  .cta:disabled { cursor: not-allowed; opacity: 0.55; }
  .cta.tone-primary { background: var(--blue); color: white; border-color: var(--blue); }
  .cta.tone-primary:hover:not(:disabled) { background: color-mix(in srgb, var(--blue) 85%, white); }
  .cta.tone-danger { background: transparent; color: var(--red); border-color: var(--red); }
  .cta.tone-danger:hover:not(:disabled) { background: color-mix(in srgb, var(--red) 12%, transparent); }
  .cta.tone-muted { background: transparent; color: var(--dim); border-color: var(--border); font-weight: 500; }

  .secondary-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin-bottom: var(--space-4);
  }
  .port-conflict {
    display: grid;
    gap: var(--space-3);
    margin-bottom: var(--space-4);
    padding: var(--space-3);
    border: 1px solid color-mix(in srgb, var(--red) 45%, var(--border));
    border-radius: var(--radius-md);
    background: color-mix(in srgb, var(--red) 8%, transparent);
  }
  .port-conflict strong {
    color: var(--red);
    font-size: var(--text-md);
  }
  .port-conflict p {
    margin: var(--space-1) 0 0;
    color: var(--dim);
    font-size: var(--text-sm);
    line-height: 1.5;
  }
  .port-conflict .secondary-btn {
    width: fit-content;
  }
  .health-failure {
    margin-bottom: var(--space-4);
    padding: var(--space-3);
    border: 1px solid color-mix(in srgb, var(--yellow) 45%, var(--border));
    border-radius: var(--radius-md);
    background: color-mix(in srgb, var(--yellow) 8%, transparent);
  }
  .health-failure strong {
    color: var(--yellow);
    font-size: var(--text-md);
  }
  .health-failure p {
    margin: var(--space-1) 0 0;
    color: var(--dim);
    font-size: var(--text-sm);
    line-height: 1.5;
  }
  .secondary-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-1) var(--space-2);
    border: 1px solid var(--border);
    background: transparent;
    color: var(--fg);
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-size: var(--text-md);
    font-family: inherit;
    line-height: 1;
    min-height: 28px;
    transition: color var(--transition-fast), border-color var(--transition-fast), background var(--transition-fast), transform var(--transition-press);
  }
  .secondary-btn:hover:not(:disabled) {
    color: var(--blue);
    border-color: var(--blue);
    background: rgba(255,255,255,0.04);
  }
  .secondary-btn:active:not(:disabled) { transform: scale(0.96); }
  .secondary-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .port-chip {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-pill);
    padding: var(--space-1) var(--space-2);
    color: var(--fg);
    font-family: monospace;
    font-size: var(--text-md);
    cursor: pointer;
    line-height: 1;
    min-height: 28px;
    transition: color var(--transition-fast), border-color var(--transition-fast), background var(--transition-fast);
  }
  .port-chip:hover:not(:disabled) { color: var(--blue); border-color: var(--blue); }
  .port-chip:disabled { cursor: default; color: var(--dim); }

  .body { padding: var(--space-4); }
  .card {
    margin-top: var(--space-4);
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  .card-title {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    margin: 0;
    padding: var(--space-2) var(--space-3);
    font-size: var(--text-md);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--dim);
    background: rgba(255, 255, 255, 0.03);
    border: 0;
    border-bottom: 1px solid var(--border);
    cursor: pointer;
    text-align: left;
    font-family: inherit;
    transition: color var(--transition-fast), background var(--transition-fast);
  }
  .card-title:hover { color: var(--fg); background: rgba(255,255,255,0.05); }
  .card-title :global(.chev) {
    transition: transform var(--transition-fast);
    flex-shrink: 0;
  }
  .card-title :global(.chev.collapsed) { transform: rotate(-90deg); }
  .card-body { padding: var(--space-3); }
  .card-body > :global(*:first-child) { margin-top: 0; }
  .kafka-topics {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .kafka-topics li {
    display: flex;
    justify-content: space-between;
    gap: var(--space-3);
    padding: 4px 0;
    font-size: var(--text-md);
  }
  .kafka-fan {
    color: var(--dim);
    white-space: nowrap;
  }
  .kafka-subhead {
    font-size: var(--text-sm);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--dim);
    margin-top: var(--space-2);
  }
  .state-reason {
    margin: var(--space-1) 0 0;
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    color: var(--red);
    word-break: break-word;
  }
</style>
