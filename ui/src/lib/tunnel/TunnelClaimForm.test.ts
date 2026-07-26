import { fireEvent, render, screen } from '@testing-library/svelte'
import { describe, expect, it, vi } from 'vitest'
import TunnelClaimForm from './TunnelClaimForm.svelte'

const { claimTunnel } = vi.hoisted(() => ({ claimTunnel: vi.fn() }))
vi.mock('./api', () => ({ claimTunnel }))

describe('TunnelClaimForm', () => {
  it('validates the port and callback path before creating a claim', async () => {
    render(TunnelClaimForm, { props: { onClaimed: vi.fn() } })

    await fireEvent.click(screen.getByRole('button', { name: 'Create tunnel' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Enter a local port')

    await fireEvent.input(screen.getByLabelText('Local port'), { target: { value: '8080' } })
    await fireEvent.input(screen.getByLabelText('Callback path'), { target: { value: 'callback' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Create tunnel' }))
    expect(screen.getByRole('alert')).toHaveTextContent('must start with /')
    expect(claimTunnel).not.toHaveBeenCalled()
  })

  it('creates a claim and refreshes the tunnel list', async () => {
    const onClaimed = vi.fn()
    claimTunnel.mockResolvedValue({ ok: true, data: { ok: true } })
    render(TunnelClaimForm, { props: { onClaimed } })

    await fireEvent.input(screen.getByLabelText('Local port'), { target: { value: '8080' } })
    await fireEvent.input(screen.getByLabelText('Callback path'), { target: { value: '/callback' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Create tunnel' }))

    expect(claimTunnel).toHaveBeenCalledWith(8080, '/callback')
    expect(onClaimed).toHaveBeenCalled()
  })

  it('surfaces gateway failures in the form', async () => {
    claimTunnel.mockResolvedValue({ ok: false, data: { error: 'path already claimed' } })
    render(TunnelClaimForm, { props: { onClaimed: vi.fn() } })

    await fireEvent.input(screen.getByLabelText('Local port'), { target: { value: '8080' } })
    await fireEvent.input(screen.getByLabelText('Callback path'), { target: { value: '/callback' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Create tunnel' }))

    expect(screen.getByRole('alert')).toHaveTextContent('path already claimed')
  })
})
