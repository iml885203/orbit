// Store is composed of three domain-scoped sub-stores:
//   store.graph  — graph dashboard view state (selection, preview, env switch progress)
//   store.daemon — daemon live state, populated from SSE (services, toggles, doctor, etc.)
//   store.ui     — ephemeral UI state (toast, modals, version banner)
//
// Domain split exists so future contributors can find state by purpose
// rather than scrolling a 50-field bag. resetForNewDaemon scope is the
// daemon sub-store; graph and ui survive reconnect.

import type { ResourceStatus, DoctorCheck, EnvToggleInfo, VersionResponse, EnvsResponse, GraphResponse } from './types.gen'

import { MAX_LINES } from './constants'

// Graph dashboard state — clears on env switch / new daemon.
class GraphStore {
  data = $state<GraphResponse | null>(null)
  // preview is a parallel read-only snapshot of another env's graph.
  // Sibling to data: live polling never touches preview; one-shot fetch
  // from EnvSwitcher writes it. When non-null, views render preview
  // instead of data and disable mutating controls.
  preview = $state<GraphResponse | null>(null)
  selectedNode = $state<string | null>(null)
  selectedEdge = $state<{ from: string; to: string; x: number; y: number } | null>(null)
  envSwitching = $state<{ target: string; total: number; phase: 'stopping' | 'loading' | 'stalled' } | null>(null)

  // The graph the canvas should render — preview wins over the live
  // poll feed. This is the contract: every view reads `active` so the
  // preview-wins rule lives in exactly one place. Changing it (e.g.
  // hide preview if stale) only touches this getter.
  get active(): GraphResponse | null {
    return this.preview ?? this.data
  }
  get isPreviewing(): boolean {
    return this.preview !== null
  }
}

// Daemon-bound live state — clears on daemon epoch change.
class DaemonStore {
  connected = $state(false)
  daemonEpoch = $state<number | null>(null)
  services = $state<Record<string, ResourceStatus>>({})
  logBuffers = $state<Record<string, string[]>>({})
  openLogs = $state<Record<string, boolean>>({})
  logModal = $state<{ target: string | null }>({ target: null })
  envToggles = $state<EnvToggleInfo[]>([])
  envs = $state<EnvsResponse | null>(null)
  doctorChecks = $state<DoctorCheck[]>([])
  doctorRunning = $state(false)
  doctorRanAt = $state('')

  get doctorPassCount() { return this.doctorChecks.filter((c) => c.status === 'pass').length }
  get doctorFailCount() { return this.doctorChecks.filter((c) => c.status === 'fail').length }
  get doctorWarnCount() { return this.doctorChecks.filter((c) => c.status === 'warn').length }

  // True when the active env is preview-only. Components gate mutation
  // affordances (up/down/stop/restart, toggles, mode switches) on this so
  // users see disabled buttons instead of 409s from the daemon.
  get activeEnvIsPreview() {
    return this.envs?.envs?.find((e) => e.current)?.previewOnly ?? false
  }
}

// Group-interior layout mode for the graph canvas: 'rectangle' packs each
// group into a near-square grid; 'extend' lays it out as wide dagre rows.
export type LayoutMode = 'rectangle' | 'extend'
const LAYOUT_MODE_KEY = 'orbit.layoutMode'
function loadLayoutMode(): LayoutMode {
  try {
    return localStorage.getItem(LAYOUT_MODE_KEY) === 'extend' ? 'extend' : 'rectangle'
  } catch {
    return 'rectangle'
  }
}

// Ephemeral UI state — survives daemon resets.
class UIStore {
  toastMessage = $state('')
  toastVisible = $state(false)
  settingsOpen = $state(false)
  envPopoverOpen = $state(false)
  showHistory = $state(false)
  version = $state<VersionResponse | null>(null)
  // Persisted across reloads so the user's layout preference sticks.
  layoutMode = $state<LayoutMode>(loadLayoutMode())

  setLayoutMode(mode: LayoutMode) {
    this.layoutMode = mode
    try {
      localStorage.setItem(LAYOUT_MODE_KEY, mode)
    } catch {
      // localStorage unavailable (private mode / SSR) — in-memory only.
    }
  }
}

class AppStore {
  graph = new GraphStore()
  daemon = new DaemonStore()
  ui = new UIStore()
}

export const store = new AppStore()

// mutationsDisabled is true when the UI must disable any control that
// hits a daemon mutation endpoint (up/down/stop/restart, env toggles,
// mode switch, edge detach, extension mutations). Two cases:
//   1. Previewing another env's graph (store.graph.isPreviewing).
//   2. The active env itself is preview-only (store.daemon.activeEnvIsPreview).
// Components use this instead of OR-ing the two flags at every call site.
export function mutationsDisabled(): boolean {
  return store.graph.isPreviewing || store.daemon.activeEnvIsPreview
}

// --- Helper functions ---
let toastTimer: ReturnType<typeof setTimeout>

export function toast(msg: string) {
  store.ui.toastMessage = msg
  store.ui.toastVisible = true
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { store.ui.toastVisible = false }, 2500)
}

export function replaceServices(svcs: ResourceStatus[]) {
  const next: Record<string, ResourceStatus> = {}
  for (const svc of svcs) next[svc.name] = svc
  store.daemon.services = next
}

export function replaceGraphData(next: GraphResponse) {
  if (store.graph.data && graphDataKey(store.graph.data) === graphDataKey(next)) return
  store.graph.data = next
}

function graphDataKey(graph: GraphResponse): string {
  return JSON.stringify({
    env: graph.env,
    previewOnly: graph.previewOnly,
    groups: graph.groups ?? [],
    nodes: graph.nodes.map(n => ({
      name: n.name,
      kind: n.kind,
      icon: n.icon ?? '',
      label: n.label ?? '',
      color: n.color ?? '',
      state: n.state,
      stateReason: n.stateReason ?? '',
      mode: n.mode ?? '',
      url: n.url ?? '',
      ports: n.ports ?? {},
      health: n.health ?? null,
      sidecars: n.sidecars ?? [],
      infraDeps: n.infraDeps ?? [],
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

// resetForNewDaemon clears all daemon-bound state when the daemon epoch
// changes (reconnect / restart). Only the "Daemon live state" group is
// reset; the other two groups are intentionally preserved:
//   - Graph dashboard: re-fetched on its own cadence; wiping it causes a
//     flash of empty canvas on reconnect.
//   - Ephemeral UI: user intent (open settings, active toast, sql mode)
//     must survive a daemon restart.
export function resetForNewDaemon() {
  store.daemon.services = {}
  store.daemon.logBuffers = {}
  store.daemon.envToggles = []
  store.daemon.envs = null
  store.daemon.doctorChecks = []
  store.daemon.logModal.target = null
}

export function appendLog(service: string, line: string) {
  if (!store.daemon.logBuffers[service]) store.daemon.logBuffers[service] = []
  store.daemon.logBuffers[service].push(line)
  if (store.daemon.logBuffers[service].length > MAX_LINES) {
    store.daemon.logBuffers[service] = store.daemon.logBuffers[service].slice(-MAX_LINES)
  }
}

export function toggleLog(name: string) {
  store.daemon.openLogs[name] = !store.daemon.openLogs[name]
}

export function isRunning(state: string): boolean {
  return state === 'healthy' || state === 'degraded' || state === 'starting' || state === 'building'
}


export function portUrl(label: string, port: number): string | null {
  if (label === 'https') return `https://localhost:${port}`
  if (['http', 'dev', 'ui'].includes(label)) return `http://localhost:${port}`
  return null
}
