import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { waitFor } from '@testing-library/svelte'
import { onDaemonReset, onEnvChanged } from '$ext'
import { resetBigQuery } from '$lib/bigquery/state.svelte'
import { store } from '$lib/stores.svelte'
import type { DevDBMetaResponse } from '$lib/types.gen'
import { refreshDevMeta } from './api'
import { dbWorkflowHidden, devStore, resetDevState, tunnelHidden } from './stores.svelte'

function response(value: unknown) {
  return new Response(JSON.stringify(value), { status: 200 })
}
function metadata(env: string, configured?: boolean): DevDBMetaResponse {
  return { environment_path: `/workspace/${env}/orbit.yaml`, environment_name: env,
    sql_server_image: 'sqlserver:fixture', workspace_root: `/workspace/${env}`,
    sql_server_service: `${env}-sql`, db_configured: configured, claim_configured: configured }
}
function pendingResponse() {
  let resolve!: (value: Response) => void
  const promise = new Promise<Response>((done) => { resolve = done })
  return { promise, resolve }
}
let reads: ReturnType<typeof pendingResponse>[]
beforeEach(() => {
  resetDevState()
  reads = []
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    if (String(input) === '/api/features/bigquery') return response({ configured: false })
    if (String(input) !== '/api/devdb/meta') throw new Error(`Unexpected request: ${String(input)}`)
    const pending = pendingResponse()
    reads.push(pending)
    return pending.promise
  })
})
afterEach(() => {
  resetDevState()
  resetBigQuery()
  store.ui.toastMessage = ''
  store.ui.toastVisible = false
  vi.restoreAllMocks()
})

describe('feature metadata request ownership', () => {
  it('rejects a pending response after feature state resets', async () => {
    const pending = refreshDevMeta({ quiet: true })
    resetDevState()
    reads[0].resolve(response(metadata('old')))
    await pending
    expect(devStore.devMeta).toBeNull()
  })

  it.each([['environment change', onEnvChanged], ['daemon reset', onDaemonReset]] as const)(
    'invalidates old metadata through the actual %s hook, even if its replacement fails',
    async (_label, changeContext) => {
      devStore.devMeta = metadata('old', true)
      const old = refreshDevMeta({ quiet: true })
      changeContext()
      const clearedImmediately = devStore.devMeta === null
      vi.spyOn(console, 'error').mockImplementation(() => {})
      reads[1].resolve(new Response('', { status: 503 }))
      reads[0].resolve(response(metadata('old', true)))
      await old
      await waitFor(() => expect(console.error).toHaveBeenCalled())
      expect(clearedImmediately).toBe(true)
      expect(devStore.devMeta).toBeNull()
      expect(dbWorkflowHidden()).toBe(false)
      expect(tunnelHidden()).toBe(false)
      expect(store.ui.toastVisible).toBe(false)
    },
  )

  it.each([['environment change', onEnvChanged], ['daemon reset', onDaemonReset]] as const)(
    'keeps the new target and explicit false gates when an old response follows %s',
    async (_label, changeContext) => {
      const old = refreshDevMeta({ quiet: true })
      changeContext()
      reads[1].resolve(response(metadata('current', false)))
      await waitFor(() => expect(devStore.devMeta?.sql_server_service).toBe('current-sql'))
      reads[0].resolve(response(metadata('old', true)))
      await old
      expect(devStore.devMeta?.sql_server_service).toBe('current-sql')
      expect(dbWorkflowHidden()).toBe(true)
      expect(tunnelHidden()).toBe(true)
    },
  )

  it('keeps the newest request within one context', async () => {
    const first = refreshDevMeta({ quiet: true })
    const latest = refreshDevMeta({ quiet: true })
    reads[1].resolve(response(metadata('newest', false)))
    await latest
    reads[0].resolve(response(metadata('older', true)))
    await first
    expect(devStore.devMeta?.environment_name).toBe('newest')
    expect(dbWorkflowHidden()).toBe(true)
    expect(tunnelHidden()).toBe(true)
  })

  it.each([true, false])('retains same-context metadata and preserves quiet=%s error signaling', async (quiet) => {
    devStore.devMeta = metadata('current', false)
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const pending = refreshDevMeta({ quiet })
    reads[0].resolve(new Response('', { status: 503 }))
    await pending
    expect(devStore.devMeta?.environment_name).toBe('current')
    expect(dbWorkflowHidden()).toBe(true)
    expect(tunnelHidden()).toBe(true)
    expect(store.ui.toastVisible).toBe(!quiet)
    if (!quiet) expect(store.ui.toastMessage).toBe('devdb metadata unavailable')
  })

  it('keeps older-daemon availability fail-open when configured fields are absent', async () => {
    const pending = refreshDevMeta({ quiet: true })
    reads[0].resolve(response(metadata('legacy')))
    await pending
    expect(devStore.devMeta?.sql_server_service).toBe('legacy-sql')
    expect(dbWorkflowHidden()).toBe(false)
    expect(tunnelHidden()).toBe(false)
  })
})
