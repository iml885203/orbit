import { fireEvent, render, screen } from '@testing-library/svelte'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnvPopover from './EnvPopover.svelte'
import { store } from '$lib/stores.svelte'

const { fetchGraph, push } = vi.hoisted(() => ({
  fetchGraph: vi.fn(),
  push: vi.fn(),
}))

vi.mock('$lib/api', () => ({ fetchGraph }))
vi.mock('svelte-spa-router', () => ({ push }))

const liveGraph = {
  env: 'development',
  nodes: [],
  edges: [],
}
const previewGraph = {
  env: 'example',
  nodes: [],
  edges: [],
}

describe('EnvPopover', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    store.ui.envPopoverOpen = true
    store.graph.data = liveGraph
    store.graph.preview = null
    store.daemon.envs = {
      running: 0,
      context: {
        kind: 'managed',
        identity: '/envs/development.yaml',
        display_name: 'development',
        config_path: '/envs/development.yaml',
        available: true,
        running: false,
        managed_selection: { name: 'development', path: '/envs/development.yaml', active: true },
      },
      envs: [
        { name: 'development.yaml', path: '/envs/development.yaml', current: true },
        { name: 'example.yaml', path: '/envs/example.yaml', current: false },
      ],
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
      managed_selection: { name: 'development', path: '/envs/development.yaml', active: false },
    }

    render(EnvPopover)

    expect(screen.getByText('Project environment')).toBeInTheDocument()
    expect(screen.getByText('/work/payments/orbit.yaml')).toBeInTheDocument()
    expect(screen.getByText('/work/payments')).toBeInTheDocument()
    expect(screen.getByText('not active')).toBeInTheDocument()
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
      managed_selection: { name: 'development', path: '/envs/development.yaml', active: false },
    }

    render(EnvPopover)

    expect(screen.getByRole('status')).toHaveTextContent('Unavailable')
    expect(screen.getByRole('button', { name: 'Preview managed environment development' })).toBeInTheDocument()
  })

  it('previews another environment and returns to Services', async () => {
    fetchGraph.mockResolvedValue(previewGraph)
    render(EnvPopover)

    await fireEvent.click(screen.getByRole('button', { name: 'example' }))

    expect(fetchGraph).toHaveBeenCalledWith('example')
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
})
