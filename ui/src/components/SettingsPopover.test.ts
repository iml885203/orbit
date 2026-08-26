import { fireEvent, render, screen } from '@testing-library/svelte'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SettingsPopover from './SettingsPopover.svelte'
import { store } from '$lib/stores.svelte'

const { push, apiPut } = vi.hoisted(() => ({ push: vi.fn(), apiPut: vi.fn() }))
vi.mock('svelte-spa-router', () => ({ push }))
vi.mock('$lib/api', () => ({ apiPut }))
vi.mock('$ext', () => ({ settingsSections: [] }))

describe('SettingsPopover', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    store.ui.settingsOpen = true
		store.ui.automaticUpdates = 'automatic'
		apiPut.mockResolvedValue({ ok: true, data: { ok: true } })
  })

  it('keeps diagnostics out of primary navigation but reachable from settings', async () => {
    render(SettingsPopover)

    await fireEvent.click(screen.getByRole('button', { name: 'Open' }))

    expect(store.ui.settingsOpen).toBe(false)
    expect(push).toHaveBeenCalledWith('/healthcheck')
  })

	it('changes the installation-wide automatic update policy', async () => {
		render(SettingsPopover)
		await fireEvent.click(screen.getByRole('button', { name: 'Off' }))

		expect(apiPut).toHaveBeenCalledWith('/api/settings', { automatic_updates: 'off' })
		expect(store.ui.automaticUpdates).toBe('off')
	})
})
