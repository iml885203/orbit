import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render } from '@testing-library/svelte'
import ServiceTable from './ServiceTable.svelte'
import { store } from '../../lib/stores.svelte'
import type { GraphResponse } from '../../lib/types.gen'

const graph: GraphResponse = {
  env: 'local',
  previewOnly: false,
  groups: [
    { name: 'shop', services: ['web', 'api'] },
    { name: 'data', services: ['redis'] },
  ],
  nodes: [
    { name: 'web', kind: 'frontend', state: 'healthy', mode: 'dev', url: 'http://localhost:5173', ports: { http: 5173 } },
    { name: 'api', kind: 'backend', state: 'healthy', mode: 'dev' },
    { name: 'redis', kind: 'infra', state: 'stopped' },
  ],
  edges: [
    { from: 'web', to: 'api', kind: 'sync', detached: false, detachable: true },
    { from: 'api', to: 'redis', kind: 'sync', detached: false, detachable: true },
  ],
}

describe('ServiceTable', () => {
  afterEach(() => {
    store.graph.preview = null
    store.daemon.envs = null
  })

  it('renders group, endpoint, and dependency data from a graph', () => {
    const { getAllByText, getByText, getByRole } = render(ServiceTable, { props: { graph, onSelect: () => {} } })

    expect(getByText('3 of 3')).toBeTruthy()
    expect(getAllByText('shop')).toHaveLength(2)
    expect(getByText('http:5173')).toBeTruthy()
    expect(getByText('api', { selector: '.deps span' })).toBeTruthy()
    expect(getByRole('button', { name: 'Start redis' })).toBeTruthy()
  })

  it('filters by resource metadata and reports an empty result', async () => {
    const { getByRole, getByText, queryByText } = render(ServiceTable, { props: { graph, onSelect: () => {} } })
    const input = getByRole('searchbox', { name: 'Filter services' })

    await fireEvent.input(input, { target: { value: 'data' } })
    expect(getByText('1 of 3')).toBeTruthy()
    expect(getByText('redis')).toBeTruthy()
    expect(queryByText('web')).toBeNull()

    await fireEvent.input(input, { target: { value: 'missing' } })
    expect(getByText('No services match “missing”.')).toBeTruthy()
  })

  it('opens service details from mouse and keyboard row activation', async () => {
    const onSelect = vi.fn()
    const { getByText } = render(ServiceTable, { props: { graph, onSelect } })
    const row = getByText('web', { selector: '.resource-name' }).closest('tr')!

    await fireEvent.click(row)
    await fireEvent.keyDown(row, { key: 'Enter' })

    expect(onSelect).toHaveBeenNthCalledWith(1, 'web')
    expect(onSelect).toHaveBeenNthCalledWith(2, 'web')
  })

  it('disables lifecycle mutations while previewing another environment', () => {
    store.graph.preview = { ...graph, env: 'preview' }
    const { getByRole, queryByRole } = render(ServiceTable, { props: { graph: store.graph.preview, onSelect: () => {} } })

    expect(getByRole('button', { name: 'Restart api' })).toBeDisabled()
    expect(getByRole('button', { name: 'Stop api' })).toBeDisabled()
    expect(getByRole('button', { name: 'Start redis' })).toBeDisabled()
    expect(queryByRole('button', { name: 'Open logs for api' })).toBeNull()
    expect(queryByRole('link', { name: 'Open web in browser' })).toBeNull()
  })
})
