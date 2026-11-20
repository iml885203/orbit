// Store is composed of three domain-scoped sub-stores:
//   store.graph  — graph dashboard view state (selection, preview, env switch progress)
//   store.daemon — daemon live state, populated from SSE (services, toggles, doctor, etc.)
//   store.ui     — ephemeral UI state (toast, modals, version banner)
//
// Domain split exists so future contributors can find state by purpose
// rather than scrolling a 50-field bag. resetForNewDaemon scope is the
// daemon sub-store; graph and ui survive reconnect.

import type { ResourceStatus, DoctorCheck, EnvToggleInfo, VersionResponse, EnvsResponse, GraphRuntimeContext } from './types.gen'

import { GraphStore } from './graph-state.svelte'
import { MAX_LINES } from './constants'

// Daemon-bound live state — clears on daemon epoch change.
class DaemonStore {
  connected = $state(false)
  daemonEpoch = $state<number | null>(null)
  instanceName = $state('')
  services = $state<Record<string, ResourceStatus>>({})
  runtimeContext = $state<GraphRuntimeContext | null>(null)
  environmentIdentity = $state('')
  logBuffers = $state<Record<string, string[]>>({})
  openLogs = $state<Record<string, boolean>>({})
  logModal = $state<{ target: string | null; loading: boolean }>({ target: null, loading: false })
  envToggles = $state<EnvToggleInfo[]>([])
  envs = $state<EnvsResponse | null>(null)
  doctorChecks = $state<DoctorCheck[]>([])
  doctorRunning = $state(false)
  doctorRanAt = $state('')

  get doctorPassCount() { return this.doctorChecks.filter((c) => c.status === 'pass').length }
  get doctorFailCount() { return this.doctorChecks.filter((c) => c.status === 'fail').length }
  get doctorWarnCount() { return this.doctorChecks.filter((c) => c.status === 'warn').length }

}

// Group-interior layout mode for the graph canvas: 'rectangle' packs each
// group into a near-square grid; 'extend' lays it out as wide dagre rows.
export type LayoutMode = 'rectangle' | 'extend'
export type ServiceView = 'graph' | 'table'
const LAYOUT_MODE_KEY = 'orbit.layoutMode'
const SERVICE_VIEW_KEY = 'orbit.serviceView'
function loadLayoutMode(): LayoutMode {
  try {
    return localStorage.getItem(LAYOUT_MODE_KEY) === 'extend' ? 'extend' : 'rectangle'
  } catch {
    return 'rectangle'
  }
}

function loadServiceView(): ServiceView {
  try {
    return localStorage.getItem(SERVICE_VIEW_KEY) === 'table' ? 'table' : 'graph'
  } catch {
    return 'graph'
  }
}

// Ephemeral UI state — survives daemon resets.
class UIStore {
  toastMessage = $state('')
  toastVisible = $state(false)
  settingsOpen = $state(false)
  envPopoverOpen = $state(false)
  sourceMigrationNoticeSeen = $state(false)
  showHistory = $state(false)
	automaticUpdates = $state<'automatic' | 'off'>('automatic')
  version = $state<VersionResponse | null>(null)
  versionRestarting = $state(false)
  // Persisted across reloads so the user's layout preference sticks.
  layoutMode = $state<LayoutMode>(loadLayoutMode())
  serviceView = $state<ServiceView>(loadServiceView())

  setLayoutMode(mode: LayoutMode) {
    this.layoutMode = mode
    try {
      localStorage.setItem(LAYOUT_MODE_KEY, mode)
    } catch {
      // localStorage unavailable (private mode / SSR) — in-memory only.
    }
  }

  setServiceView(view: ServiceView) {
    this.serviceView = view
    try {
      localStorage.setItem(SERVICE_VIEW_KEY, view)
    } catch {
      // localStorage unavailable (private mode / SSR) — in-memory only.
    }
  }
}

class AppStore {
  daemon = new DaemonStore()
  graph = new GraphStore(() => this.daemon, toast)
  ui = new UIStore()
}

export const store = new AppStore()

// mutationsDisabled keeps another env's on-disk graph a read-only preview.
// Activating that environment makes its mutation controls available.
export function mutationsDisabled(): boolean {
  return !store.graph.canMutate
}

// --- Helper functions ---
let toastTimer: ReturnType<typeof setTimeout>

export function toast(msg: string) {
  store.ui.toastMessage = msg
  store.ui.toastVisible = true
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { store.ui.toastVisible = false }, 2500)
}

export function replaceServices(svcs: ResourceStatus[], context: GraphRuntimeContext | null = null) {
  store.daemon.runtimeContext = context
  const next: Record<string, ResourceStatus> = {}
  for (const svc of svcs) next[svc.name] = svc
  store.daemon.services = next
}

// resetForNewDaemon clears all daemon-bound state when the daemon epoch
// changes (reconnect / restart). Only the "Daemon live state" group is
// reset; the other two groups are intentionally preserved:
//   - Graph dashboard: re-fetched on its own cadence; wiping it causes a
//     flash of empty canvas on reconnect.
//   - Ephemeral UI: user intent (open settings, active toast, sql mode)
//     must survive a daemon restart.
export function resetForNewDaemon() {
  store.daemon.runtimeContext = null
  store.daemon.environmentIdentity = ''
  store.graph.disconnect()
  store.daemon.services = {}
  store.daemon.logBuffers = {}
  store.daemon.envToggles = []
  store.daemon.envs = null
  store.daemon.doctorChecks = []
  store.daemon.logModal.target = null
  store.daemon.logModal.loading = false
}

export function appendLog(service: string, line: string) {
  if (!store.daemon.logBuffers[service]) store.daemon.logBuffers[service] = []
  store.daemon.logBuffers[service].push(line)
  if (store.daemon.logBuffers[service].length > MAX_LINES) {
    store.daemon.logBuffers[service] = store.daemon.logBuffers[service].slice(-MAX_LINES)
  }
}

export function openLogViewer(service: string) {
  store.daemon.logModal = { target: service, loading: true }
}

export function hydrateLogs(service: string, snapshot: string[]) {
  const live = store.daemon.logBuffers[service] ?? []
  let overlap = Math.min(snapshot.length, live.length)
  while (overlap > 0) {
    const snapshotTail = snapshot.slice(snapshot.length - overlap)
    const liveHead = live.slice(0, overlap)
    if (snapshotTail.every((line, index) => line === liveHead[index])) break
    overlap--
  }
  store.daemon.logBuffers[service] = [...snapshot, ...live.slice(overlap)].slice(-MAX_LINES)
}


export function isRunning(state: string): boolean {
  return state === 'healthy' || state === 'degraded' || state === 'starting' || state === 'building'
}
