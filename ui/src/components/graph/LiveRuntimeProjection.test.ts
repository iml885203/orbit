import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'
import ServiceTable from './ServiceTable.svelte'
import ServiceNode from './ServiceNode.svelte'
import GroupNode from './GroupNode.svelte'
import NodeDrawerSelection from './NodeDrawerSelection.test.svelte'
import { replaceServices, store } from '$lib/stores.svelte'
import type { GraphResponse, ResourceStatus } from '$lib/types.gen'

const owner = { epoch: 101, generation: 1, config_path: '/qa/orbit.yaml' }
function resource(state = 'healthy'): ResourceStatus {
  return { name: 'web', kind: 'service', mode: 'dev', state, restart_count: 0, external_restart_count: 0 }
}
beforeEach(() => {
  store.graph.dispose()
  store.daemon.daemonEpoch = owner.epoch
  store.daemon.runtimeContext = owner
  store.daemon.connected = true
  store.graph.data = { env: 'qa', runtime: owner, groups: [{ name: 'app', services: ['web'] }],
    nodes: [{ name: 'web', kind: 'frontend', runtime_kind: 'service', mode: 'dev', state: 'degraded',
      stateReason: 'old dependency failure', failureKind: 'dependency', blockedBy: 'api' },
    { name: 'api', kind: 'backend', runtime_kind: 'service', state: 'stopped' }], edges: [] }
  replaceServices([resource()], owner)
  store.graph.publish(store.graph.data)
  vi.spyOn(globalThis, 'fetch').mockImplementation(async () => new Response(JSON.stringify({ env: [] })))
})
afterEach(() => {
  cleanup()
  store.graph.dispose()
  store.daemon.services = {}
  store.daemon.connected = false
  store.daemon.runtimeContext = null
  store.daemon.daemonEpoch = null
  vi.restoreAllMocks()
})

function table(graph: GraphResponse = store.graph.data!) {
  return render(ServiceTable, { props: { graph, onSelect: () => {} } })
}

describe('guarded live graph views', () => {
  it('clears stale blockers in table actions without mutating the graph snapshot', () => {
    const original = store.graph.data
    table()
    expect(screen.getByRole('button', { name: 'Restart web' })).toBeEnabled()
    expect(store.graph.data).toBe(original)
    expect(store.graph.data?.nodes[0].blockedBy).toBe('api')
    expect(store.graph.data?.nodes[0].stateReason).toBe('old dependency failure')
  })

  it.each(['preview', 'offline', 'switching', 'daemon epoch', 'generation', 'config path', 'missing owner',
    'nonmember', 'runtime kind', 'runtime mode', 'missing resource'])('keeps snapshot data for %s', scenario => {
    let graph = store.graph.data!
    switch (scenario) {
      case 'preview': store.graph.preview = { ...graph, env: 'preview' }; graph = store.graph.preview; break
      case 'offline': store.daemon.connected = false; break
      case 'switching': store.graph.envSwitching = { target: 'other', total: 0, phase: 'loading' }; break
      case 'daemon epoch': store.daemon.daemonEpoch = 202; break
      case 'generation': store.daemon.runtimeContext = { ...owner, generation: 2 }; break
      case 'config path': store.daemon.runtimeContext = { ...owner, config_path: '/other/orbit.yaml' }; break
      case 'missing owner': graph.runtime = undefined; break
      case 'nonmember': graph = { ...graph, nodes: graph.nodes.map(node => ({ ...node })) }; break
      case 'runtime kind': store.daemon.services.web = { ...resource(), kind: 'container' }; break
      case 'runtime mode': store.daemon.services.web = { ...resource(), mode: 'container' }; break
      case 'missing resource': store.daemon.services = {}; break
    }
    table(graph)
    expect(screen.getByText('degraded')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Restart web' })).not.toBeInTheDocument()
  })

  it('updates a canvas service node without changing layout data', () => {
    const original = store.graph.data!.nodes[0]
    render(ServiceNode, { props: { data: original, id: 'web' } })
    expect(screen.getByRole('button', { name: 'Restart web' })).toBeInTheDocument()
    expect(original.state).toBe('degraded')
    expect(original.blockedBy).toBe('api')
  })

  it('updates group lifecycle controls from the live resource state', async () => {
    render(GroupNode, { props: { data: { name: 'app', serviceCount: 1 } } })
    replaceServices([resource('building')], owner)
    await tick()
    expect(screen.getByRole('button', { name: 'Start app group' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Stop app group' })).toBeDisabled()
    replaceServices([resource('stopped')], owner)
    await tick()
    expect(screen.getByRole('button', { name: 'Start app group' })).toBeEnabled()
    expect(store.graph.data?.nodes[0].state).toBe('degraded')
  })

  it('clears stale drawer recovery actions using the same guarded projection', () => {
    store.graph.selectedNode = 'web'
    render(NodeDrawerSelection)
    expect(screen.getByRole('button', { name: 'Stop' })).toBeEnabled()
    expect(screen.queryByRole('button', { name: 'Start api' })).not.toBeInTheDocument()
    expect(store.graph.data?.nodes[0].blockedBy).toBe('api')
  })
  it.each([
    ['exited', 'compiler diagnostic', 'exited — compiler diagnostic'],
    ['', 'evidence alone', 'evidence alone'],
    ['same detail', 'same detail', 'same detail'],
    ['reason only', '', 'reason only'],
  ])('preserves current failure summary %s / %s and clears it on recovery', async (reason, evidence, expected) => {
    replaceServices([{ ...resource('degraded'), state_reason: reason, failure_evidence: evidence }], owner)
    store.graph.selectedNode = 'web'
    render(NodeDrawerSelection)
    expect(screen.getByText(expected)).toBeInTheDocument()
    replaceServices([resource()], owner)
    await tick()
    expect(screen.queryByText(expected)).not.toBeInTheDocument()
    expect(screen.queryByText('old dependency failure')).not.toBeInTheDocument()
  })

  it.each(['direct', 'transitive', 'own first'])('keeps %s current port conflict actionable and clears it after recovery', async scenario => {
    const inherited = { port: 5000, resource: 'api', inspect_command: 'inspect-api-port' }
    const own = { port: 5173, resource: 'web', inspect_command: 'inspect-web-port' }
    store.graph.data!.nodes[0].portConflict = inherited
    const original = store.graph.data
    const root = { ...resource('pending'), pending_dependencies: ['api'],
      port_conflict: scenario === 'own first' ? own : undefined }
    const api: ResourceStatus = { name: 'api', kind: 'service', state: 'pending', restart_count: 0, external_restart_count: 0,
      pending_dependencies: scenario === 'transitive' ? ['db'] : [],
      port_conflict: scenario === 'transitive' ? undefined : inherited }
    const db: ResourceStatus = { name: 'db', kind: 'container', state: 'degraded', restart_count: 0,
      external_restart_count: 0, port_conflict: inherited }
    replaceServices([root, api, db], owner)
    table()
    store.graph.selectedNode = 'web'
    const clipboard = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    try {
      render(NodeDrawerSelection)
      expect(screen.queryByRole('button', { name: 'Start web' })).not.toBeInTheDocument()
      const conflict = scenario === 'own first' ? own : inherited
      expect(screen.getByText(`Port ${conflict.port} is already in use`)).toBeInTheDocument()
      await fireEvent.click(screen.getByRole('button', { name: 'Copy inspection command' }))
      expect(writeText).toHaveBeenCalledWith(conflict.inspect_command)
      replaceServices([resource('stopped'), { ...api, state: 'healthy', pending_dependencies: [], port_conflict: undefined }], owner)
      await tick()
      expect(screen.getByRole('button', { name: 'Start web' })).toBeEnabled()
      expect(screen.queryByRole('button', { name: 'Copy inspection command' })).not.toBeInTheDocument()
      expect(store.graph.data).toBe(original)
      expect(store.graph.data?.nodes[0].portConflict).toEqual(inherited)
    } finally {
      if (clipboard) Object.defineProperty(navigator, 'clipboard', clipboard)
      else Reflect.deleteProperty(navigator, 'clipboard')
    }
  })

  it('handles cyclic and absent pending dependency statuses without inventing a conflict', () => {
    store.graph.data!.nodes[0].portConflict = { port: 5000, resource: 'old', inspect_command: 'old-inspection' }
    replaceServices([{ ...resource('pending'), pending_dependencies: ['api', 'absent'] },
      { name: 'api', kind: 'service', state: 'pending', restart_count: 0, external_restart_count: 0,
        pending_dependencies: ['web'] }], owner)
    table()
    expect(screen.getByRole('button', { name: 'Start web' })).toBeEnabled()
  })

})
