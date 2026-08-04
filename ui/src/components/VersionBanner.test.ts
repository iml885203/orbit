import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { tick } from 'svelte'
import VersionBanner from './VersionBanner.svelte'
import { store } from '$lib/stores.svelte'

const { fetchVersion, restartForUpdate } = vi.hoisted(() => ({
  fetchVersion: vi.fn(),
  restartForUpdate: vi.fn(),
}))

vi.mock('$lib/api', () => ({ fetchVersion, restartForUpdate }))

const installed = 'v0.9.1 (2026-08-04 12:00:00 +0800)'

describe('VersionBanner', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    store.ui.versionRestarting = false
    store.ui.version = {
      running: 'v0.8.0 (2026-08-03 12:00:00 +0800)',
      on_disk: installed,
      on_disk_path: '/usr/local/bin/orbit',
      update_available: true,
    }
  })

  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it('presents an installed update as ready to apply with secondary build details', () => {
    render(VersionBanner)

    expect(screen.getByRole('status')).toHaveTextContent('Orbit v0.9.1 is ready')
    expect(screen.getByRole('status')).toHaveTextContent('running resources will be restored')
    expect(screen.getByRole('button', { name: 'Restart now' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Later' })).toBeInTheDocument()
    expect(screen.getByText('Build details')).toBeInTheDocument()
  })

  it('dismisses only the current installed version', async () => {
    render(VersionBanner)
    await fireEvent.click(screen.getByRole('button', { name: 'Later' }))
    expect(screen.queryByRole('button', { name: 'Restart now' })).not.toBeInTheDocument()

    store.ui.version = {
      ...store.ui.version!,
      on_disk: 'v0.9.2 (2026-08-05 12:00:00 +0800)',
    }
    await tick()
    expect(screen.getByRole('button', { name: 'Restart now' })).toBeInTheDocument()
  })

  it('shows reconnect progress and confirms the running target version', async () => {
    restartForUpdate.mockResolvedValue({ ok: true, data: { ok: true } })
    fetchVersion.mockResolvedValue({ running: installed, update_available: false })
    render(VersionBanner)

    await fireEvent.click(screen.getByRole('button', { name: 'Restart now' }))
    expect(screen.getByRole('button', { name: 'Restarting…' })).toHaveAttribute('aria-busy', 'true')
    expect(store.ui.versionRestarting).toBe(true)

    await vi.advanceTimersByTimeAsync(1000)
    expect(screen.getByText('Orbit updated to', { exact: false })).toHaveTextContent('v0.9.1')
    expect(store.ui.version?.running).toBe(installed)
    expect(store.ui.versionRestarting).toBe(false)
  })

  it('offers retry and CLI recovery when restart scheduling fails', async () => {
    restartForUpdate.mockResolvedValue({ ok: false, data: { error: 'launcher unavailable' } })
    render(VersionBanner)

    await fireEvent.click(screen.getByRole('button', { name: 'Restart now' }))

    expect(screen.getByRole('status')).toHaveTextContent('Orbit didn’t restart. launcher unavailable')
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy command' })).toBeInTheDocument()
    expect(store.ui.versionRestarting).toBe(false)
  })

  it('bounds reconnect attempts before showing recovery actions', async () => {
    restartForUpdate.mockResolvedValue({ ok: true, data: { ok: true } })
    fetchVersion.mockResolvedValue(null)
    render(VersionBanner)

    await fireEvent.click(screen.getByRole('button', { name: 'Restart now' }))
    await vi.advanceTimersByTimeAsync(7000)

    expect(fetchVersion).toHaveBeenCalledTimes(3)
    expect(screen.getByRole('status')).toHaveTextContent('Orbit did not reconnect with the expected version')
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy command' })).toBeInTheDocument()
  })

  it('aborts probes that never return so reconnect still reaches recovery', async () => {
    restartForUpdate.mockResolvedValue({ ok: true, data: { ok: true } })
    fetchVersion.mockImplementation((signal?: AbortSignal) => new Promise((resolve) => {
      signal?.addEventListener('abort', () => resolve(null), { once: true })
    }))
    render(VersionBanner)

    await fireEvent.click(screen.getByRole('button', { name: 'Restart now' }))
    await vi.advanceTimersByTimeAsync(13000)

    expect(fetchVersion).toHaveBeenCalledTimes(3)
    expect(screen.getByRole('status')).toHaveTextContent('Orbit did not reconnect with the expected version')
    expect(store.ui.versionRestarting).toBe(false)
  })
})
