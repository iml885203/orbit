import { fireEvent, render, screen } from '@testing-library/svelte'
import { beforeEach, describe, expect, it } from 'vitest'
import { store } from '$lib/stores.svelte'
import ServicesViewToolbar from './ServicesViewToolbar.svelte'

describe('ServicesViewToolbar', () => {
  beforeEach(() => {
    store.ui.setLayoutMode('rectangle')
    store.ui.setServiceView('graph')
  })

  it('keeps graph layout and view controls in one toolbar', () => {
    render(ServicesViewToolbar, { props: { hasGroups: true } })

    const toolbar = screen.getByLabelText('Services graph controls')
    expect(toolbar).toContainElement(screen.getByRole('group', { name: 'Group layout' }))
    expect(toolbar).toContainElement(screen.getByRole('group', { name: 'Services view' }))
  })

  it('lets each graph control change its independent setting', async () => {
    render(ServicesViewToolbar, { props: { hasGroups: true } })

    await fireEvent.click(screen.getByRole('button', { name: 'Extended layout' }))
    expect(store.ui.layoutMode).toBe('extend')
    expect(store.ui.serviceView).toBe('graph')

    await fireEvent.click(screen.getByRole('button', { name: 'Table view' }))
    expect(store.ui.layoutMode).toBe('extend')
    expect(store.ui.serviceView).toBe('table')
  })

  it('hides layout controls when the graph has no groups', () => {
    render(ServicesViewToolbar, { props: { hasGroups: false } })

    expect(screen.queryByRole('group', { name: 'Group layout' })).not.toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'Services view' })).toBeInTheDocument()
  })
})
