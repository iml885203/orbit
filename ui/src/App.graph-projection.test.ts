import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/svelte'
import { tick } from 'svelte'
import App from './App.svelte'
import { store } from '$lib/stores.svelte'
import { optimisticDetach } from '$lib/graphActions'
import type { GraphResponse, ResourceStatus, StatusResponse } from '$lib/types.gen'
vi.mock('$ext', () => ({ onAppMount: () => () => {}, onDaemonState: () => {}, onEnvChanged: () => {}, onDaemonReset: () => {}, navItems: [], routes: {}, nodeDrawerPanels: [], settingsSections: [] }))
class StatusSource {
  static current: StatusSource
  onerror: (() => void) | null = null
  private listeners = new Map<string, EventListener[]>()
  constructor() { StatusSource.current = this }
  addEventListener(type: string, callback: EventListener) { this.listeners.set(type, [...(this.listeners.get(type) ?? []), callback]) }
  close() {}
  emit(value: StatusResponse) { const event = new MessageEvent('status', { data: JSON.stringify(value) }); for (const callback of this.listeners.get('status') ?? []) callback(event) }
}
const owner = { epoch: 101, generation: 1, config_path: '/qa/orbit.yaml' }
const resources: ResourceStatus[] = ['api', 'web'].map(name => ({ name, kind: 'service', state: 'healthy', restart_count: 0, external_restart_count: 0 }))
const statusA: StatusResponse = { ...owner, resources, context: { kind: 'project', identity: '/qa/orbit.yaml', display_name: 'qa', config_path: owner.config_path, available: true, running: true } }
const graphA: GraphResponse = { env: 'qa', runtime: owner, nodes: [{ name: 'api', kind: 'backend', runtime_kind: 'service', state: 'healthy' }, { name: 'web', kind: 'frontend', runtime_kind: 'service', state: 'healthy' }], edges: [{ from: 'web', to: 'api', kind: 'dependency', detachable: true, detached: true }] }
const graphB: GraphResponse = { ...graphA, nodes: [{ ...graphA.nodes[0], state: 'stopped' }, { ...graphA.nodes[1], state: 'degraded', blockedBy: 'api', stateReason: 'dependency api unavailable' }], edges: [{ ...graphA.edges[0], detached: false }] }
let serverGraph: GraphResponse
let graphReads: number
const response = (value: unknown) => new Response(JSON.stringify(value))
beforeEach(() => {
  store.graph.dispose(); store.graph.data = null; store.daemon.daemonEpoch = null; store.daemon.runtimeContext = null; store.daemon.environmentIdentity = ''; store.daemon.services = {}; store.daemon.connected = false; store.ui.serviceView = 'table'; store.ui.version = null; window.location.hash = '#/'
  serverGraph = graphA; graphReads = 0
  vi.stubGlobal('EventSource', StatusSource)
  vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
    switch (String(input)) {
      case '/api/graph': graphReads++; return response(serverGraph)
      case '/api/edges/web/api': serverGraph = graphB; return response({ ok: true, graph: graphB })
      case '/api/env-toggles': return response([])
      case '/api/settings': return response({ env_toggles: {}, user_env: {} })
      case '/api/envs': return response({ current: 'qa', running: 2, sources: [], context: statusA.context })
      case '/api/tracing/status': return response({ configured: false })
      case '/api/version': return response({ running: 'v1.6.0', on_disk: 'v1.6.0', update_available: false })
      default: throw new Error(`Unexpected request ${String(input)}`)
    }
  })
})
afterEach(() => { cleanup(); store.graph.dispose(); vi.restoreAllMocks(); vi.unstubAllGlobals() })
describe('actual App same-owner runtime projection', () => {
  it('uses unchanged live SSE A for table actions after an intermediate edge-read B without replacing topology', async () => {
    render(App); StatusSource.current.emit(statusA)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Restart web' })).toBeEnabled())
    expect(await optimisticDetach(store.graph.data!.edges[0], false)).toBe(true)
    await waitFor(() => expect(store.graph.data?.nodes.find(node => node.name === 'web')?.blockedBy).toBe('api'))
    const intermediateBase = store.graph.data; const readsBeforeStatus = graphReads
    serverGraph = { ...graphA, edges: graphB.edges }; StatusSource.current.emit(statusA); await tick()
    expect(store.daemon.services.web.state).toBe('healthy')
    expect(store.daemon.services.api.state).toBe('healthy')
    expect(store.graph.data).toBe(intermediateBase)
    expect(store.graph.data?.nodes.find(node => node.name === 'web')?.state).toBe('degraded')
    expect(graphReads).toBe(readsBeforeStatus)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Restart web' })).toBeEnabled())
  })
})
