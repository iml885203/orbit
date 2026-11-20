import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, waitFor } from '@testing-library/svelte'
import { tick } from 'svelte'
import App from './App.svelte'
import { refreshDevMeta } from '$lib/devdb/api'
import { devStore, resetDevState } from '$lib/devdb/stores.svelte'
import { resetBigQuery } from '$lib/bigquery/state.svelte'
import { store } from '$lib/stores.svelte'
import { tracing } from '$lib/tracing.svelte'
import type { StatusResponse } from '$lib/types.gen'

class StatusSource {
  static current: StatusSource
  onerror: (() => void) | null = null
  private listeners = new Map<string, EventListener[]>()
  constructor() { StatusSource.current = this }
  addEventListener(type: string, callback: EventListener) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), callback])
  }
  close() {}
  emit(status: StatusResponse) {
    const event = new MessageEvent('status', { data: JSON.stringify(status) })
    for (const callback of this.listeners.get('status') ?? []) callback(event)
  }
}
function response(value: unknown) { return new Response(JSON.stringify(value), { status: 200 }) }
function metadata(project: string) {
  return { environment_path: `/workspace/${project}/orbit.yaml`, environment_name: 'local',
    sql_server_image: '', workspace_root: `/workspace/${project}`, sql_server_service: `${project}-sql`,
    db_configured: true, claim_configured: false }
}
function status(project: string, generation = 1, epoch = 101): StatusResponse {
  const configPath = `/workspace/${project}/orbit.yaml`
  return { epoch, generation, config_path: configPath, resources: [],
    context: { kind: 'project', identity: project, display_name: 'local', config_path: configPath,
      project_root: `/workspace/${project}`, available: true, running: false } }
}
let project: string
let deferMetadata: boolean
let metadataRequests: number
let pending: Array<(value: Response) => void>
beforeEach(() => {
  project = 'a'
  deferMetadata = false
  metadataRequests = 0
  pending = []
  resetDevState()
  store.graph.data = null
  store.graph.preview = null
  store.graph.selectedNode = null
  store.daemon.daemonEpoch = null
  store.daemon.envs = null
  store.daemon.services = {}
  store.ui.version = null
  window.location.hash = '#/'
  vi.stubGlobal('EventSource', StatusSource)
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    switch (String(input)) {
      case '/api/devdb/meta':
        metadataRequests++
        if (deferMetadata) return new Promise<Response>((resolve) => { pending.push(resolve) })
        return response(metadata(project))
      case '/api/features/bigquery': return response({ configured: false })
      case '/api/graph': return response({ env: 'local', nodes: [], edges: [] })
      case '/api/env-toggles': return response([])
      case '/api/settings': return response({ env_toggles: {}, user_env: {} })
      case '/api/envs': return response({ running: 0, sources: [], context: status(project).context })
      case '/api/tracing/status': return response({ configured: false })
      case '/api/version': return response({ running: 'test', on_disk: 'test', update_available: false })
      default: throw new Error(`Unexpected request: ${String(input)}`)
    }
  })
})
afterEach(async () => {
  cleanup()
  for (const resolve of pending) resolve(response(metadata('discarded')))
  await tick()
  resetDevState()
  resetBigQuery()
  tracing.reset()
  store.graph.data = null
  store.graph.preview = null
  store.daemon.daemonEpoch = null
  store.daemon.envs = null
  store.daemon.services = {}
  store.daemon.connected = false
  store.ui.version = null
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
async function mountLiveApp() {
  render(App)
  await waitFor(() => expect(devStore.devMeta?.sql_server_service).toBe('a-sql'))
  const beforeFirstStatus = metadataRequests
  StatusSource.current.emit(status('a'))
  await tick()
  expect(metadataRequests).toBe(beforeFirstStatus + 1)
  await waitFor(() => expect(store.graph.data?.env).toBe('local'))
}

describe('App feature metadata context delivery', () => {
  it.each([
    ['project switch', status('b', 2)],
    ['config path', { ...status('a'), config_path: '/workspace/b/orbit.yaml',
      context: { ...status('a').context, config_path: '/workspace/b/orbit.yaml' } }],
    ['context identity', { ...status('a'), context: { ...status('a').context, identity: 'b' } }],
  ])('invalidates a same-name %s before a failed replacement and rejects the old response', async (_label, nextStatus) => {
    await mountLiveApp()
    deferMetadata = true
    const old = refreshDevMeta({ quiet: true })
    const beforeSwitch = metadataRequests
    project = 'b'
    StatusSource.current.emit(nextStatus)
    await tick()
    const cleared = devStore.devMeta === null
    const refetched = metadataRequests === beforeSwitch + 1
    vi.spyOn(console, 'error').mockImplementation(() => {})
    pending[1]?.(new Response('', { status: 503 }))
    pending[0](response(metadata('a')))
    await old
    await tick()
    expect(refetched).toBe(true)
    expect(cleared).toBe(true)
    expect(devStore.devMeta).toBeNull()
    expect(store.ui.toastVisible).toBe(false)
  })

  it('ignores repeated live context and preview changes but refreshes a new generation', async () => {
    await mountLiveApp()
    const before = metadataRequests
    StatusSource.current.emit(status('a'))
    store.graph.preview = { env: 'different-preview', nodes: [], edges: [] }
    await tick()
    expect(metadataRequests).toBe(before)
    StatusSource.current.emit(status('a', 2))
    await tick()
    expect(metadataRequests).toBe(before + 1)
  })

  it('refetches once through daemon reset when epoch changes', async () => {
    await mountLiveApp()
    const before = metadataRequests
    StatusSource.current.emit(status('a', 1, 202))
    await tick()
    expect(metadataRequests).toBe(before + 1)
  })
})
