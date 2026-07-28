import { describe, expect, it } from 'vitest'
import { render } from '@testing-library/svelte'
import LogModal from './LogModal.svelte'

describe('LogModal', () => {
  it('explains that buffered logs are loading', () => {
    const { getByRole, queryByText } = render(LogModal, {
      props: {
        service: 'api',
        lines: [],
        loading: true,
        onClose: () => {},
      },
    })

    expect(getByRole('status').textContent).toContain('Loading buffered logs')
    expect(queryByText('No log output yet.')).toBeNull()
  })

  it('distinguishes an empty log from a loading log', () => {
    const { getByRole, queryByText } = render(LogModal, {
      props: {
        service: 'api',
        lines: [],
        onClose: () => {},
      },
    })

    expect(getByRole('status').textContent).toContain('No log output yet')
    expect(queryByText('Loading buffered logs…')).toBeNull()
  })
})
