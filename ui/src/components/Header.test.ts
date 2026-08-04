import { cleanup, render, screen, waitFor } from '@testing-library/svelte'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import Header from './Header.svelte'
import { store } from '$lib/stores.svelte'

describe('Header runtime identity', () => {
  beforeEach(() => {
    store.daemon.connected = true
    store.daemon.instanceName = ''
    store.daemon.envs = null
    store.graph.preview = null
    store.ui.version = null
    store.ui.versionRestarting = false
  })

  afterEach(() => {
    cleanup()
    document.title = 'Orbit Dashboard'
  })

  it('keeps the default runtime header unchanged', async () => {
    render(Header)

    expect(screen.getByRole('heading', { name: 'Orbit' })).toBeInTheDocument()
    expect(screen.queryByLabelText(/^Instance /)).not.toBeInTheDocument()
    await waitFor(() => expect(document.title).toBe('Orbit Dashboard'))
  })

  it('identifies a named runtime in the header and browser tab', async () => {
    store.daemon.instanceName = 'checkout-a'
    render(Header)

    expect(screen.getByLabelText('Instance checkout-a')).toHaveAttribute('title', 'checkout-a')
    await waitFor(() => expect(document.title).toBe('checkout-a · Orbit Dashboard'))

    store.daemon.connected = false
    expect(screen.getByLabelText('Instance checkout-a')).toBeInTheDocument()
  })

  it('keeps a long instance name available when its badge is truncated', () => {
    const instanceName = 'a'.repeat(63)
    store.daemon.instanceName = instanceName
    render(Header)

    const badge = screen.getByLabelText(`Instance ${instanceName}`)
    expect(badge).toHaveAttribute('title', instanceName)
    expect(badge.querySelector('.instance-name')).toHaveTextContent(instanceName)
  })

  it('labels an expected update handoff as reconnecting instead of disconnected', () => {
    store.daemon.connected = false
    store.ui.versionRestarting = true
    render(Header)

    expect(screen.getByRole('status', { name: 'Restarting and reconnecting' })).toBeInTheDocument()
    expect(screen.queryByRole('status', { name: 'Disconnected' })).not.toBeInTheDocument()
  })
})
