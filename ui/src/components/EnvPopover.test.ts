import { fireEvent, render, screen } from '@testing-library/svelte'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnvPopover from './EnvPopover.svelte'
import envPopoverSource from './EnvPopover.svelte?raw'
import mainPageSource from '../routes/MainPage.svelte?raw'
import { store } from '$lib/stores.svelte'

const { fetchGraph, mutateSource, push } = vi.hoisted(() => ({
  fetchGraph: vi.fn(),
	mutateSource: vi.fn(),
  push: vi.fn(),
}))

vi.mock('$lib/api', () => ({ fetchGraph, fetchEnvs: vi.fn(), mutateSource }))
vi.mock('svelte-spa-router', () => ({ push }))

const liveGraph = {
  env: 'default/development',
  nodes: [],
  edges: [],
}
const previewGraph = {
  env: 'default/example',
  nodes: [],
  edges: [],
}

describe('EnvPopover', () => {
  beforeEach(() => {
    vi.clearAllMocks()
	mutateSource.mockResolvedValue({ ok: true, data: {} })
    store.ui.envPopoverOpen = true
    store.graph.data = liveGraph
    store.graph.preview = null
    store.ui.sourceMigrationNoticeSeen = false
    store.daemon.envs = {
      running: 0,
      context: {
        kind: 'managed',
        identity: 'default/development',
        display_name: 'development',
        config_path: '/envs/development.yaml',
        available: true,
        running: false,
        managed_selection: { identity: 'default/development', name: 'development', path: '/envs/development.yaml', active: true },
      },
      sources: [{ name: 'default', type: 'git', location: 'https://example.com/envs.git', environments: [
        { identity: 'default/development', name: 'development', path: '/envs/development.yaml', selected: true, running: false },
        { identity: 'default/example', name: 'example', path: '/envs/example.yaml', selected: false, running: false },
      ] }],
    }
  })

  it('shows project provenance and the inactive managed selection', () => {
    if (!store.daemon.envs) throw new Error('env fixture missing')
    store.daemon.envs.context = {
      kind: 'project',
      identity: '/work/payments/orbit.yaml',
      display_name: 'payments',
      config_path: '/work/payments/orbit.yaml',
      project_root: '/work/payments',
      available: true,
      running: true,
      managed_selection: { identity: 'default/development', name: 'development', path: '/envs/development.yaml', active: false },
    }

    render(EnvPopover)

    expect(screen.getByText('Project environment')).toBeInTheDocument()
    expect(screen.getByText('/work/payments/orbit.yaml')).toBeInTheDocument()
    expect(screen.getByText('/work/payments')).toBeInTheDocument()
    expect(screen.getByText('not active')).toBeInTheDocument()
  })

  it('covers page controls while the environment selector is open', () => {
    const layer = (source: string, selector: string) => Number(
      source.match(new RegExp(`\\.${selector}\\s*\\{[^}]*z-index:\\s*(\\d+)`, 's'))?.[1],
    )
    const toolbarLayer = layer(mainPageSource, 'view-toolbar')
    const backdropLayer = layer(envPopoverSource, 'backdrop')
    const popoverLayer = layer(envPopoverSource, 'popover')

    expect(backdropLayer).toBeGreaterThan(toolbarLayer)
    expect(popoverLayer).toBeGreaterThan(backdropLayer)
  })

  it('offers the inactive managed environment when a project config is unavailable', () => {
    if (!store.daemon.envs) throw new Error('env fixture missing')
    store.daemon.envs.context = {
      kind: 'project',
      identity: '/work/missing/orbit.yaml',
      display_name: 'missing',
      config_path: '/work/missing/orbit.yaml',
      project_root: '/work/missing',
      available: false,
      running: false,
      managed_selection: { identity: 'default/development', name: 'development', path: '/envs/development.yaml', active: false },
    }

    render(EnvPopover)

    expect(screen.getByRole('status')).toHaveTextContent('Unavailable')
    expect(screen.getByRole('button', { name: 'Preview managed environment default/development' })).toBeInTheDocument()
  })

  it('previews another environment and returns to Services', async () => {
    fetchGraph.mockResolvedValue(previewGraph)
    render(EnvPopover)

    await fireEvent.click(screen.getByRole('button', { name: 'example' }))

    expect(fetchGraph).toHaveBeenCalledWith('default/example')
    expect(store.graph.preview).toStrictEqual(previewGraph)
    expect(store.ui.envPopoverOpen).toBe(false)
    expect(push).toHaveBeenCalledWith('/')
  })

  it('returns to the live environment without fetching it again', async () => {
    store.graph.preview = previewGraph
    render(EnvPopover)

    await fireEvent.click(screen.getByRole('button', { name: /development/i }))

    expect(fetchGraph).not.toHaveBeenCalled()
    expect(store.graph.preview).toBeNull()
    expect(push).toHaveBeenCalledWith('/')
  })

  it('keeps source settings behind a dedicated management view', async () => {
    render(EnvPopover)

    expect(screen.queryByRole('button', { name: 'Sync' })).not.toBeInTheDocument()
    await fireEvent.click(screen.getByRole('button', { name: /Manage sources/ }))
    expect(screen.getByRole('region', { name: 'Environment source management' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Sync' })).toBeInTheDocument()
  })

  it('shows the offline migration outcome until it is dismissed', async () => {
    if (!store.daemon.envs) throw new Error('env fixture missing')
    store.daemon.envs.migration = { source_name: 'default', location: 'https://example.com/envs.git', ref: 'main', cached_environments: 2, selection_preserved: true, workspace_preserved: true, offline: true }
    render(EnvPopover)

    expect(screen.getByRole('status', { name: 'Environment source migration complete' })).toHaveTextContent('default from https://example.com/envs.git at main offline')
    await fireEvent.click(screen.getByRole('button', { name: 'Dismiss migration summary' }))
	expect(mutateSource).toHaveBeenCalledWith({ action: 'ack_migration' })
    expect(screen.queryByRole('status', { name: 'Environment source migration complete' })).not.toBeInTheDocument()
  })

	it('keeps the migration summary visible when acknowledgement fails', async () => {
	  if (!store.daemon.envs) throw new Error('env fixture missing')
	  store.daemon.envs.migration = { source_name: 'default', location: 'https://example.com/envs.git', cached_environments: 1, selection_preserved: false, workspace_preserved: false, offline: true }
	  mutateSource.mockResolvedValue({ ok: false, data: { error: 'disk unavailable' } })
	  render(EnvPopover)
	  await fireEvent.click(screen.getByRole('button', { name: 'Dismiss migration summary' }))
	  expect(screen.getByRole('status', { name: 'Environment source migration complete' })).toBeInTheDocument()
	})
})
