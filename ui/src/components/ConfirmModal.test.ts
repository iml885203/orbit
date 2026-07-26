import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render } from '@testing-library/svelte'
import ConfirmModal from './ConfirmModal.svelte'

describe('ConfirmModal keyboard shortcuts', () => {
  it('confirms a non-danger action with Enter', async () => {
    const onConfirm = vi.fn()
    render(ConfirmModal, { props: { open: true, title: 'Confirm?', message: 'Proceed.', onConfirm, onCancel: vi.fn() } })

    await fireEvent.keyDown(window, { key: 'Enter' })

    expect(onConfirm).toHaveBeenCalledOnce()
  })

  it('does not confirm a danger action with Enter', async () => {
    const onConfirm = vi.fn()
    render(ConfirmModal, { props: { open: true, title: 'Delete?', message: 'This is destructive.', danger: true, onConfirm, onCancel: vi.fn() } })

    await fireEvent.keyDown(window, { key: 'Enter' })

    expect(onConfirm).not.toHaveBeenCalled()
  })

  it('cancels a danger action with Escape', async () => {
    const onCancel = vi.fn()
    render(ConfirmModal, { props: { open: true, title: 'Delete?', message: 'This is destructive.', danger: true, onConfirm: vi.fn(), onCancel } })

    await fireEvent.keyDown(window, { key: 'Escape' })

    expect(onCancel).toHaveBeenCalledOnce()
  })
})
