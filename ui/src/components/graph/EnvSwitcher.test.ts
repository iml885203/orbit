import { render, screen } from '@testing-library/svelte'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EnvSwitcher from './EnvSwitcher.svelte'
import { store } from '$lib/stores.svelte'

vi.mock('$lib/api', () => ({
  apiPost: vi.fn(),
  fetchGraph: vi.fn(),
  switchEnv: vi.fn(),
}))

const liveGraph = { env: 'development', previewOnly: false, nodes: [], edges: [] }
const previewGraph = { env: 'example', previewOnly: false, nodes: [], edges: [] }

describe('EnvSwitcher', () => {
  beforeEach(() => {
    store.graph.data = liveGraph
    store.graph.preview = null
    store.daemon.services = {}
    store.daemon.envs = {
      running: 0,
      envs: [
        { name: 'development.yaml', path: '/envs/development.yaml', current: true, previewOnly: false },
        { name: 'example.yaml', path: '/envs/example.yaml', current: false, previewOnly: false },
      ],
    }
  })

  it('shows lifecycle controls without a second environment selector', () => {
    render(EnvSwitcher)

    expect(screen.getByText('development')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Up All' })).toBeInTheDocument()
    expect(screen.queryByRole('tab')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'example' })).not.toBeInTheDocument()
  })

  it('shows explicit activation controls only while previewing', () => {
    store.graph.preview = previewGraph
    render(EnvSwitcher)

    expect(screen.getByText('Previewing')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Use this env' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Exit preview' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Up All' })).not.toBeInTheDocument()
  })
})
