import { SvelteMap, SvelteSet } from 'svelte/reactivity'
import type { EnvironmentSwitchResponse, GraphEdge, GraphNode, GraphResponse, GraphRuntimeContext, ResourceStatus, StatusResponse } from './types.gen'
import { detachEdge, fetchGraph } from './api'

type LiveAuthority = { connected: boolean; daemonEpoch: number | null; runtimeContext: GraphRuntimeContext | null; environmentIdentity: string; services: Record<string, ResourceStatus> }
type SwitchProgress = { target: string; total: number; phase: 'stopping' | 'loading' | 'stalled' }
type EdgeIntent = { confirmed: boolean }

function sameGraphOwner(a: GraphRuntimeContext | null | undefined, b: GraphRuntimeContext | null | undefined): boolean {
  return !!a && !!b && a.epoch === b.epoch && a.generation === b.generation && a.config_path === b.config_path
}

function resourceFailureSummary(status: ResourceStatus): string {
  const reason = status.state_reason ?? ''
  const evidence = status.failure_evidence ?? ''
  if (!evidence || evidence === reason) return reason
  return reason ? `${reason} — ${evidence}` : evidence
}

function graphPortConflictFor(name: string, statuses: Record<string, ResourceStatus>, seen = new SvelteSet<string>()): ResourceStatus['port_conflict'] {
  if (seen.has(name)) return undefined
  seen.add(name)
  const status = statuses[name]
  if (!status) return undefined
  if (status.port_conflict) return status.port_conflict
  for (const dependency of status.pending_dependencies ?? []) {
    const conflict = graphPortConflictFor(dependency, statuses, seen)
    if (conflict) return conflict
  }
  return undefined
}

export class GraphStore {
  data = $state<GraphResponse | null>(null)
  preview = $state<GraphResponse | null>(null)
  selectedNode = $state<string | null>(null)
  selectedEdge = $state<{ from: string; to: string; x: number; y: number } | null>(null)
  envSwitching = $state<SwitchProgress | null>(null)
  freshness = $state<'current' | 'reconciling' | 'unavailable'>('reconciling')
  private observedOwner = ''
  private statusKey = ''
  private lifecycle = 0
  private writeAuthority = 0
  private returningStatus = false
  private waitingForRead = $state(false)
  private mutationBoundary = 0
  private previewIntent = 0
  private previewAbort: AbortController | null = null
  private read: { abort: AbortController; timer: ReturnType<typeof setTimeout> } | null = null
  private refreshPending = false
  private retryTimer: ReturnType<typeof setTimeout> | null = null
  private retries = 0
  private switchIntent: { owner: GraphRuntimeContext | null; identity: string; acknowledged: boolean } | null = null
  private switchTimer: ReturnType<typeof setTimeout> | null = null
  private edges = new SvelteMap<string, EdgeIntent>()
  private edgeQueues = new SvelteMap<string, Promise<void>>()

  constructor(private authority: () => LiveAuthority, private notify: (message: string) => void) {}

  get active(): GraphResponse | null { return this.preview ?? this.data }
  get isPreviewing(): boolean { return this.preview !== null }
  get canMutate(): boolean {
    const live = this.authority()
    return !this.isPreviewing && !this.envSwitching && !this.waitingForRead && live.connected &&
      !!live.runtimeContext?.config_path && sameGraphOwner(this.data?.runtime, live.runtimeContext)
  }

  runtimeFor(node: GraphNode): ResourceStatus | null {
    const live = this.authority()
    const owner = this.data?.runtime
    if (this.isPreviewing || this.envSwitching || !live.connected || !owner?.config_path ||
      owner.epoch !== live.daemonEpoch || !sameGraphOwner(owner, live.runtimeContext) ||
      !this.data?.nodes.includes(node)) return null
    const kind = node.runtime_kind ?? (node.kind === 'infra' ? 'container' : 'service')
    const status = live.services[node.name]
    return status?.kind === kind && status.mode === node.mode ? status : null
  }

  projectNode(node: GraphNode): GraphNode {
    const status = this.runtimeFor(node)
    if (!status) return node
    return { ...node,
      state: status.state, stateReason: resourceFailureSummary(status), failureKind: status.failure_kind,
      blockedBy: status.blocked_by, portConflict: graphPortConflictFor(node.name, this.authority().services),
      logsAvailable: status.logs_available, health: status.health_progress,
      url: status.url, ports: status.ports,
      restart_count: status.restart_count, external_restart_count: status.external_restart_count,
      last_restart: status.last_restart, startup_time: status.startup_time, uptime: status.uptime,
    }
  }

  observeStatus(status: StatusResponse, refresh = false) {
    const statusKey = statusGraphKey(status)
    refresh ||= statusKey !== this.statusKey
    this.statusKey = statusKey
    const key = JSON.stringify([status.epoch, status.generation, status.config_path])
    if (key !== this.observedOwner) {
      const previous = this.observedOwner
      this.observedOwner = key
      this.invalidateRead()
      this.writeAuthority++
      this.edges.clear()
      this.edgeQueues.clear()
      this.waitingForRead = true
      if (previous) this.clearSelection()
      refresh = true
    }
    if (refresh || this.returningStatus) this.refresh()
    this.returningStatus = false
    this.finishSwitch()
  }

  disconnect() {
    this.invalidateRead()
    this.returningStatus = true
    this.restoreConfirmedEdges()
    this.waitingForRead = true
    this.freshness = 'unavailable'
  }

  dispose() {
    this.disconnect()
    this.writeAuthority++
    this.edges.clear()
    this.edgeQueues.clear()
    this.observedOwner = ''
    this.statusKey = ''
    this.exitPreview()
    if (this.switchTimer) clearTimeout(this.switchTimer)
    this.switchTimer = null
    this.switchIntent = null
    this.envSwitching = null
  }

  private invalidateRead() {
    this.lifecycle++
    if (this.retryTimer) clearTimeout(this.retryTimer)
    this.retryTimer = null
    this.retries = 0
    if (this.read) {
      clearTimeout(this.read.timer)
      this.read.abort.abort()
      this.read = null
    }
    this.refreshPending = false
  }

  refresh(retrying = false) {
    if (!retrying) {
      if (this.retryTimer) clearTimeout(this.retryTimer)
      this.retryTimer = null
      this.retries = 0
    }
    this.refreshPending = true
    if (this.authority().runtimeContext && !this.authority().connected) return
    if (this.read || this.edges.size) return
    this.refreshPending = false
    const lifecycle = this.lifecycle
    const boundary = this.mutationBoundary
    const abort = new AbortController()
    const lane = { abort, timer: setTimeout(() => {
      if (this.read !== lane) return
      this.read = null
      abort.abort()
      this.freshness = 'unavailable'
      this.retryRead()
    }, 10_000) }
    this.read = lane
    void fetchGraph(undefined, abort.signal).then(graph => {
      if (this.read !== lane || lifecycle !== this.lifecycle) return
      if (!graph) {
        this.freshness = 'unavailable'
        this.refreshPending = false
        this.retryRead()
      }
      else if (boundary === this.mutationBoundary && !this.edges.size) {
        if (!this.publish(graph)) {
          this.refreshPending = false
          this.retryRead()
        }
      }
      else this.refreshPending = true
    }).finally(() => {
      clearTimeout(lane.timer)
      if (this.read !== lane) return
      this.read = null
      if (this.refreshPending) this.refresh()
    })
  }

  private retryRead() {
    if (this.retries >= 3 || this.retryTimer) return
    const lifecycle = this.lifecycle
    const delay = 1000 * 2 ** this.retries++
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null
      if (lifecycle === this.lifecycle) this.refresh(true)
    }, delay)
  }

  publish(next: GraphResponse): boolean {
    const live = this.authority()
    if (this.edges.size) {
      this.refreshPending = true
      this.freshness = 'reconciling'
      return false
    }
    if (live.runtimeContext && (!live.connected || !sameGraphOwner(next.runtime, live.runtimeContext))) {
      this.freshness = 'unavailable'
      return false
    }
    if (this.data && graphDataKey(this.data) === graphDataKey(next)) this.data.runtime = next.runtime
    else this.data = next
    this.freshness = live.connected && sameGraphOwner(next.runtime, live.runtimeContext) ? 'current' : 'reconciling'
    if (this.freshness === 'current') this.waitingForRead = false
    this.finishSwitch()
    return true
  }

  async requestPreview(identity: string): Promise<boolean> {
    this.previewAbort?.abort()
    const intent = ++this.previewIntent
    if (identity === this.authority().environmentIdentity) {
      this.exitPreview()
      return true
    }
    const abort = new AbortController()
    this.previewAbort = abort
    const timer = setTimeout(() => {
      if (intent !== this.previewIntent || abort.signal.aborted) return
      this.notify(`Preview ${identity} timed out. Select it again to retry.`)
      abort.abort()
    }, 10_000)
    try {
      const graph = await fetchGraph(identity, abort.signal)
      if (intent !== this.previewIntent || abort.signal.aborted) return false
      if (!graph) {
        this.notify(`Failed to load ${identity}`)
        return false
      }
      this.preview = graph
      this.clearSelection()
      return true
    } finally {
      clearTimeout(timer)
      if (this.previewAbort === abort) this.previewAbort = null
    }
  }

  private cancelPreviewRequest() {
    this.previewIntent++
    this.previewAbort?.abort()
    this.previewAbort = null
  }

  exitPreview() {
    this.cancelPreviewRequest()
    this.preview = null
    this.clearSelection()
  }

  private clearSelection() {
    this.selectedNode = null
    this.selectedEdge = null
  }

  beginSwitch(identity: string, total: number): boolean {
    if (this.switchIntent) {
      this.notify('A switch is still pending. Wait for the replacement daemon or reload the dashboard.')
      return false
    }
    if (this.switchTimer) clearTimeout(this.switchTimer)
    this.invalidateRead()
    this.cancelPreviewRequest()
    this.switchIntent = { owner: this.authority().runtimeContext, identity, acknowledged: false }
    this.envSwitching = { target: identity, total, phase: 'stopping' }
    this.switchTimer = setTimeout(() => {
      if (this.envSwitching) this.envSwitching.phase = 'stalled'
    }, 30_000)
    return true
  }

  receiveSwitch(ok: boolean, _response?: EnvironmentSwitchResponse) {
    if (!this.switchIntent) return
    if (!ok) {
      this.clearSwitch()
      this.restoreConfirmedEdges()
      this.waitingForRead = true
      this.freshness = 'reconciling'
      this.refresh()
      return
    }
    this.exitPreview()
    this.switchIntent.acknowledged = true
    if (this.envSwitching && this.envSwitching.phase !== 'stalled') this.envSwitching.phase = 'loading'
    this.finishSwitch()
  }

  private finishSwitch() {
    const intent = this.switchIntent
    const live = this.authority()
    if (!intent?.acknowledged || !live.connected || live.environmentIdentity !== intent.identity ||
      !live.runtimeContext || sameGraphOwner(live.runtimeContext, intent.owner) ||
      !sameGraphOwner(this.data?.runtime, live.runtimeContext)) return
    this.clearSwitch()
  }

  private clearSwitch() {
    if (this.switchTimer) clearTimeout(this.switchTimer)
    this.switchTimer = null
    this.switchIntent = null
    this.envSwitching = null
  }

  async changeEdge(edge: GraphEdge, target: boolean): Promise<boolean> {
    const owner = this.authority().runtimeContext
    if (!owner || !this.canMutate) return false
    const authority = this.writeAuthority
    const key = `${edge.from}\0${edge.to}`
    const previousIntent = this.edges.get(key)
    const intent: EdgeIntent = { confirmed: previousIntent?.confirmed ?? edge.detached }
    this.edges.set(key, intent)
    this.mutationBoundary++
    this.freshness = 'reconciling'
    this.applyEdge(edge, target)
    const previous = this.edgeQueues.get(key) ?? Promise.resolve()
    let finish!: () => void
    const turn = new Promise<void>(resolve => { finish = resolve })
    this.edgeQueues.set(key, turn)
    try {
      await previous
      if (authority !== this.writeAuthority || !sameGraphOwner(owner, this.authority().runtimeContext) || !this.authority().connected) return false
      const { ok, data } = await detachEdge(this.data?.env ?? '', edge.from, edge.to, target, owner)
      if (authority !== this.writeAuthority || !sameGraphOwner(owner, this.authority().runtimeContext)) return ok
      const latest = this.edges.get(key)
      if (!ok) {
        if (latest === intent) this.applyEdge(edge, intent.confirmed)
        this.notify(data?.error || 'Failed to update edge')
        return false
      }
      const confirmed = data?.graph?.edges.find(candidate => candidate.from === edge.from && candidate.to === edge.to)
      if (confirmed && sameGraphOwner(data?.graph?.runtime, owner)) {
        if (latest) latest.confirmed = confirmed.detached
        if (latest === intent) this.applyEdge(edge, confirmed.detached)
        this.notify(data?.message || (target ? 'Detached' : 'Reattached'))
      } else {
        this.freshness = 'unavailable'
        this.notify('Edge updated; graph reconciliation unavailable. Refresh graph to confirm.')
      }
      return true
    } finally {
      finish()
      if (authority === this.writeAuthority) {
        if (this.edgeQueues.get(key) === turn) this.edgeQueues.delete(key)
        if (this.edges.get(key) === intent) this.edges.delete(key)
        this.mutationBoundary++
        if (!this.edges.size) this.refresh()
      }
    }
  }

  private restoreConfirmedEdges() {
    for (const [key, intent] of this.edges) {
      const [from, to] = key.split('\0')
      this.applyEdge({ from, to }, intent.confirmed)
    }
  }

  private applyEdge(edge: Pick<GraphEdge, 'from' | 'to'>, detached: boolean) {
    const index = this.data?.edges.findIndex(candidate => candidate.from === edge.from && candidate.to === edge.to) ?? -1
    if (this.data && index >= 0) this.data.edges[index] = { ...this.data.edges[index], detached }
  }
}

function graphDataKey(graph: GraphResponse): string {
  return JSON.stringify({
    env: graph.env,
    groups: graph.groups ?? [],
    nodes: graph.nodes.map(n => ({
      name: n.name,
      kind: n.kind,
      runtimeKind: n.runtime_kind ?? '',
      icon: n.icon ?? '',
      label: n.label ?? '',
      color: n.color ?? '',
      state: n.state,
      stateReason: n.stateReason ?? '',
      failureKind: n.failureKind ?? '',
      blockedBy: n.blockedBy ?? '',
      portConflict: n.portConflict ?? null,
      logsAvailable: n.logsAvailable ?? false,
      mode: n.mode ?? '',
      url: n.url ?? '',
      ports: n.ports ?? {},
      health: n.health ?? null,
      sidecars: n.sidecars ?? [],
      kafka: n.kafka ?? null,
    })),
    edges: graph.edges.map(e => ({
      from: e.from,
      to: e.to,
      kind: e.kind,
      topic: e.topic ?? '',
      detached: e.detached,
      detachable: e.detachable,
      env_vars: e.env_vars ?? [],
    })),
  })
}


  function statusGraphKey(resp: StatusResponse): string {
    const resources = (resp.resources ?? [])
      .map((s) => [
        s.name, s.state, s.state_reason ?? '', s.mode ?? '', s.url ?? '',
        s.logs_available ? 'logs' : '',
        JSON.stringify(s.ports ?? {}),
        s.health_progress ? `${s.health_progress.attempts}/${s.health_progress.last_err ?? ''}` : '',
      ].join('|'))
      .join(';')
    return JSON.stringify([resp.epoch, resp.generation, resp.config_path, resources])
  }
