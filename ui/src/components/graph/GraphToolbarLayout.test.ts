import { describe, expect, it } from 'vitest'
import graphViewSource from './GraphView.svelte?raw'
import mainPageSource from '../../routes/MainPage.svelte?raw'

describe('Services graph toolbar layout', () => {
  it('reserves the page view toggle width before the graph layout toggle', () => {
    expect(mainPageSource).toContain('--services-view-toggle-width: 64px')
    expect(graphViewSource).toContain(
      'margin-right: calc(var(--services-view-toggle-width) + var(--space-3))',
    )
  })
})
