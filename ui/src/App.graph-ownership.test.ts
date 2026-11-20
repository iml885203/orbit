import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import { tick } from 'svelte'
import App from './App.svelte'
import { optimisticDetach } from '$lib/graphActions'
import { store, resetForNewDaemon } from '$lib/stores.svelte'
import type { GraphResponse, StatusResponse } from '$lib/types.gen'

vi.mock('$ext', () => ({
  onAppMount: () => () => {}, onDaemonState: () => {},
  onEnvChanged: () => {}, onDaemonReset: () => {},
  navItems: [], routes: {}, nodeDrawerPanels: [], settingsSections: [],
}))

class StatusSource {
  static current: StatusSource
  onerror: (() => void) | null = null
  private listeners = new Map<string, EventListener[]>()
  constructor() { StatusSource.current = this }
  addEventListener(type: string, callback: EventListener) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), callback])
  }
  close() {}
  emit(value: StatusResponse) {
    const event = new MessageEvent('status', { data: JSON.stringify(value) })
    for (const callback of this.listeners.get('status') ?? []) callback(event)
  }
}
const owner = (project: string, generation: number) => ({
  epoch: 101, generation, config_path: `/workspace/${project}/orbit.yaml`,
})
const graph = (project: string, generation: number): GraphResponse => ({
  env: project, runtime: owner(project, generation), nodes: [], edges: [],
})
const status = (project: string, generation: number): StatusResponse => ({
  ...owner(project, generation), resources: [],
  context: { kind: 'project', identity: project, display_name: project,
    config_path: `/workspace/${project}/orbit.yaml`, project_root: `/workspace/${project}`,
    available: true, running: false },
})
const response = (value: unknown) => new Response(JSON.stringify(value))
let graphs: Array<(value: Response) => void>
let mutations: Array<(value: Response) => void>
let switches: Array<(value: Response) => void>
beforeEach(() => {
  graphs = []
  mutations = []
  switches = []
  resetForNewDaemon()
  store.daemon.daemonEpoch = null
  store.daemon.connected = false
  store.graph.data = null
  store.graph.preview = null
  store.graph.envSwitching = null
  store.graph.selectedNode = null
  store.ui.version = null
  store.ui.serviceView = 'table'
  window.location.hash = '#/'
  vi.stubGlobal('EventSource', StatusSource)
  vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
    switch (String(input)) {
      case '/api/graph': return new Promise<Response>(resolve => graphs.push(resolve))
      case '/api/envs/current': return new Promise<Response>(resolve => switches.push(resolve))
      case '/api/edges/web/api': return new Promise<Response>(resolve => mutations.push(resolve))
      case '/api/env-toggles': return response([])
      case '/api/settings': return response({ env_toggles: {}, user_env: {} })
      case '/api/envs': return response({ current: 'a', running: 0, sources: [], context: status('a', 1).context })
      case '/api/tracing/status': return response({ configured: false })
      case '/api/version': return response({ running: 'test', on_disk: 'test', update_available: false })
      default: throw new Error(`Unexpected request: ${String(input)}`)
    }
  })
})
afterEach(async () => {
  cleanup()
  for (const resolve of switches) resolve(response({ ok: false, error: 'cleanup' }))
  for (const resolve of mutations) resolve(response({ ok: false, error: 'cleanup' }))
  for (const resolve of graphs) resolve(response(graph('cleanup', 99)))
  await tick()
  resetForNewDaemon()
  store.daemon.daemonEpoch = null
  store.daemon.connected = false
  store.graph.data = null
  store.graph.preview = null
  store.graph.envSwitching = null
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('App graph response ownership', () => {
  it('refreshes identical returning status before restoring real mutation controls', async () => {
    render(App)
    await waitFor(() => expect(graphs).toHaveLength(1))
    const snapshot = { ...graph('a', 1), nodes: [{ name: 'web', kind: 'frontend', state: 'stopped' }] }
    graphs[0](response(snapshot))
    StatusSource.current.emit(status('a', 1))
    await waitFor(() => expect(graphs).toHaveLength(2))
    graphs[1](response(snapshot))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Start web' })).toBeEnabled())
    StatusSource.current.onerror?.()
    await tick()
    await new Promise(resolve => setTimeout(resolve, 3100))
    StatusSource.current.emit(status('a', 1))
    await waitFor(() => expect(graphs).toHaveLength(3))
    expect(screen.getByRole('button', { name: 'Start web' })).toBeDisabled()
    graphs[2](response(snapshot))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Start web' })).toBeEnabled())
  })

  it.each(['bootstrap', 'disconnected'])('does not switch a preview without connected authority: %s', async phase => {
    render(App)
    await waitFor(() => expect(graphs).toHaveLength(1))
    graphs[0](response(graph('a', 1)))
    if (phase === 'disconnected') {
      StatusSource.current.emit(status('a', 1))
      await waitFor(() => expect(graphs).toHaveLength(2))
      graphs[1](response(graph('a', 1)))
      await waitFor(() => expect(store.graph.canMutate).toBe(true))
      StatusSource.current.onerror?.()
      await tick()
    }
    store.graph.preview = { env: 'b', nodes: [], edges: [] }
    await tick()
    const use = screen.getByRole('button', { name: 'Use this env' })
    await fireEvent.click(use)
    expect(switches).toHaveLength(0)
    expect(use).toBeDisabled()
    expect(store.graph.beginSwitch('b', 0)).toBe(false)
  })

  it('waits through actual switch ACK and old same-identity status until the replacement graph arrives', async () => {
    render(App)
    await waitFor(() => expect(graphs).toHaveLength(1))
    graphs[0](response(graph('a', 1)))
    StatusSource.current.emit(status('a', 1))
    await waitFor(() => expect(graphs).toHaveLength(2))
    graphs[1](response(graph('a', 1)))
    await waitFor(() => expect(store.graph.canMutate).toBe(true))
    store.graph.preview = { env: 'a', nodes: [], edges: [] }
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Use this env' }))
    await waitFor(() => expect(switches).toHaveLength(1))
    switches[0](response({ ok: true, message: 'Restart scheduled' }))
    await waitFor(() => expect(store.graph.envSwitching?.phase).toBe('loading'))
    StatusSource.current.emit(status('a', 1))
    await tick()
    expect(store.graph.envSwitching?.phase).toBe('loading')
    expect(store.graph.canMutate).toBe(false)
    const count = graphs.length
    StatusSource.current.emit({ ...status('a', 1), epoch: 202 })
    await waitFor(() => expect(graphs.length).toBeGreaterThan(count))
    for (let index = count; index < graphs.length; index++) {
      graphs[index](response({ ...graph('a', 1), runtime: { ...owner('a', 1), epoch: 202 } }))
    }
    await waitFor(() => expect(store.graph.envSwitching).toBeNull())
    expect(store.graph.canMutate).toBe(true)
    expect(store.graph.data?.runtime?.epoch).toBe(202)
  })

  it('keeps real service controls read-only through bootstrap and disconnect', async () => {
    render(App)
    await waitFor(() => expect(graphs).toHaveLength(1))
    const bootstrap = { ...graph('a', 1), nodes: [{ name: 'web', kind: 'frontend', state: 'stopped' }] }
    graphs[0](response(bootstrap))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Start web' })).toBeDisabled())
    StatusSource.current.emit(status('a', 1))
    await waitFor(() => expect(graphs).toHaveLength(2))
    graphs[1](response(bootstrap))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Start web' })).toBeEnabled())
    StatusSource.current.onerror?.()
    await tick()
    expect(screen.getByRole('button', { name: 'Start web' })).toBeDisabled()
    expect(store.graph.data?.env).toBe('a')
  })

  it('keeps a successful edge ack when a newer background graph GET fails', async () => {
    render(App)
    await waitFor(() => expect(graphs).toHaveLength(1))
    graphs[0](response(graph('a', 1)))
    await waitFor(() => expect(store.graph.data?.env).toBe('a'))
    StatusSource.current.emit(status('a', 1))
    await waitFor(() => expect(graphs).toHaveLength(2))
    const currentGraph: GraphResponse = { ...graph('a', 1), edges: [
      { from: 'web', to: 'api', kind: 'dependency', detachable: true, detached: false },
    ] }
    graphs[1](response(currentGraph))
    await waitFor(() => expect(store.graph.data?.edges).toHaveLength(1))
    const operation = optimisticDetach(store.graph.data!.edges[0], true)
    await waitFor(() => expect(mutations).toHaveLength(1))
    StatusSource.current.emit({ ...status('a', 1), resources: [
      { name: 'web', kind: 'service', state: 'starting', restart_count: 0, external_restart_count: 0 },
    ] })
    await tick()
    expect(graphs).toHaveLength(2)
    mutations[0](response({ ok: true, graph: {
      ...currentGraph, edges: [{ ...currentGraph.edges[0], detached: true }],
    } }))
    expect(await operation).toBe(true)
    await waitFor(() => expect(graphs).toHaveLength(3))
    vi.spyOn(console, 'error').mockImplementation(() => {})
    graphs[2](new Response('', { status: 503 }))
    await tick()
    expect(store.graph.data?.edges[0].detached).toBe(true)
    expect(store.graph.data?.runtime).toEqual(owner('a', 1))
  })

  it('keeps B when a graph request from previous A finishes last', async () => {
    render(App)
    await waitFor(() => expect(graphs).toHaveLength(1))
    graphs[0](response(graph('a', 1)))
    await waitFor(() => expect(store.graph.data?.env).toBe('a'))

    StatusSource.current.emit(status('a', 1))
    await waitFor(() => expect(graphs).toHaveLength(2))
    const delayedA = graphs[1]
    StatusSource.current.emit(status('b', 2))
    await waitFor(() => expect(graphs).toHaveLength(3))
    graphs[2](response(graph('b', 2)))
    await waitFor(() => expect(store.graph.data?.env).toBe('b'))

    delayedA(response(graph('a', 1)))
    await tick()
    await new Promise<void>(resolve => setTimeout(resolve, 0))
    expect(store.daemon.runtimeContext).toEqual(owner('b', 2))
    expect(store.graph.data?.runtime).toEqual(owner('b', 2))
    expect(store.graph.data?.env).toBe('b')
  })
})
