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
  previewOnly: false,
  nodes: [],
  edges: [],
}
const previewGraph = {
  env: 'example',
  previewOnly: false,
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
      envs: [
        { name: 'development.yaml', path: '/envs/development.yaml', current: true, previewOnly: false },
        { name: 'example.yaml', path: '/envs/example.yaml', current: false, previewOnly: false },
      ],
    }
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
