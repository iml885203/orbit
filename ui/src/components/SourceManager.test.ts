import { fireEvent, render, screen, within } from '@testing-library/svelte'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SourceManager from './SourceManager.svelte'
import { store } from '$lib/stores.svelte'

const { fetchEnvs, mutateSource } = vi.hoisted(() => ({
  fetchEnvs: vi.fn(),
  mutateSource: vi.fn(),
}))

vi.mock('$lib/api', () => ({ fetchEnvs, mutateSource }))

const context = {
  kind: 'managed' as const,
  identity: 'team/development',
  display_name: 'development',
  config_path: '/cache/development.yaml',
  available: true,
  running: false,
}

function source(overrides: Record<string, unknown> = {}) {
  return {
    name: 'team',
    type: 'git',
    location: 'https://example.com/team/envs.git',
    resolved_ref: 'main',
    commit: '1234567890abcdef',
    last_sync_at: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
    environments: [
      { identity: 'team/development', name: 'development', path: '/cache/development.yaml', selected: true, running: false },
      { identity: 'team/staging', name: 'staging', path: '/cache/staging.yaml', selected: false, running: false },
    ],
    ...overrides,
  }
}

describe('SourceManager', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    store.daemon.envs = { running: 0, context, sources: [source()] }
    fetchEnvs.mockResolvedValue(store.daemon.envs)
    mutateSource.mockResolvedValue({ ok: true, data: { message: 'done' } })
  })

  it('shows source freshness, revision, commit, and environment count', () => {
    render(SourceManager, { props: { onback: vi.fn() } })

    expect(screen.getByText('Synced 1 hour ago')).toBeInTheDocument()
    expect(screen.getByText('main · 12345678')).toBeInTheDocument()
    expect(screen.getByText('2 environments')).toBeInTheDocument()
  })

  it('guides a Git or local source through validation and sync', async () => {
    render(SourceManager, { props: { onback: vi.fn() } })
    await fireEvent.click(screen.getByRole('button', { name: 'Add source' }))

    expect(screen.getByText('Orbit validates the source, syncs it, and shows the environments it finds.')).toBeInTheDocument()
    await fireEvent.click(screen.getByRole('radio', { name: /Local directory/ }))
    expect(screen.getByLabelText('Local directory')).toBeInTheDocument()
    expect(screen.queryByLabelText(/Branch or tag/)).not.toBeInTheDocument()
  })

  it('disables removal when the source owns the running environment', async () => {
    store.daemon.envs = { running: 1, context: { ...context, running: true }, sources: [source({
      environments: [{ identity: 'team/development', name: 'development', path: '/cache/development.yaml', selected: true, running: true }],
    })] }
    render(SourceManager, { props: { onback: vi.fn() } })

    expect(screen.getByRole('button', { name: 'Remove' })).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent('Stop or switch away from team/development')
  })

  it('allows removing the first source without choosing a replacement', async () => {
    store.daemon.envs = { running: 0, context, sources: [source(), source({ name: 'other', environments: [] })] }
    render(SourceManager, { props: { onback: vi.fn() } })
    const teamCard = screen.getByText('team').closest('article')!

    expect(within(teamCard).getByRole('button', { name: 'Remove' })).toBeEnabled()
  })

  it('explains selected-environment impact and preserves a local directory', async () => {
    store.daemon.envs = { running: 0, context, sources: [source({ type: 'local', location: '/work/envs' })] }
    render(SourceManager, { props: { onback: vi.fn() } })
    await fireEvent.click(screen.getByRole('button', { name: 'Remove' }))

    const confirmation = screen.getByRole('group', { name: 'Remove source team' })
    expect(confirmation).toHaveTextContent('selected environment team/development will be cleared')
    expect(confirmation).toHaveTextContent('local directory and its files will remain untouched')
  })

  it('does not claim an unselected source will clear the selection', async () => {
    store.daemon.envs = { running: 0, context, sources: [source({ environments: [] })] }
    render(SourceManager, { props: { onback: vi.fn() } })
    await fireEvent.click(screen.getByRole('button', { name: 'Remove' }))

    expect(screen.getByRole('group', { name: 'Remove source team' })).not.toHaveTextContent('selected environment')
  })

  it('surfaces action failures without hiding the current source state', async () => {
    mutateSource.mockResolvedValue({ ok: false, data: { error: 'network unavailable' } })
    render(SourceManager, { props: { onback: vi.fn() } })
    await fireEvent.click(screen.getByRole('button', { name: 'Sync' }))

    expect(screen.getByRole('alert')).toHaveTextContent('network unavailable')
    expect(screen.getByText('main · 12345678')).toBeInTheDocument()
	  expect(fetchEnvs).toHaveBeenCalled()
  })

  it('warns when a mutation may have completed but refresh fails', async () => {
    fetchEnvs.mockRejectedValue(new Error('offline'))
    render(SourceManager, { props: { onback: vi.fn() } })
    await fireEvent.click(screen.getByRole('button', { name: 'Sync' }))
    expect(screen.getByRole('alert')).toHaveTextContent('may have completed')
  })

  it('preserves a precise mutation error when refreshing also fails', async () => {
	mutateSource.mockResolvedValue({ ok: false, data: { error: 'invalid ref' } })
	fetchEnvs.mockRejectedValue(new Error('offline'))
	render(SourceManager, { props: { onback: vi.fn() } })
	await fireEvent.click(screen.getByRole('button', { name: 'Sync' }))
	expect(screen.getByRole('alert')).toHaveTextContent('invalid ref')
	expect(screen.getByRole('alert')).not.toHaveTextContent('may have completed')
  })
})
