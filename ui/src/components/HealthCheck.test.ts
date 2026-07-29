import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, waitFor } from '@testing-library/svelte'
import HealthCheck from './HealthCheck.svelte'
import { store } from '../lib/stores.svelte'

describe('HealthCheck', () => {
  beforeEach(() => {
    store.daemon.doctorChecks = []
    store.daemon.doctorRunning = false
    store.daemon.doctorRanAt = ''
    vi.restoreAllMocks()
  })

  it('leads with problems and keeps successful checks collapsed', () => {
    store.daemon.doctorChecks = [
      {
        name: 'Python',
        status: 'pass',
        message: 'Python 3.13 is available',
      },
      {
        name: 'Packages (api)',
        status: 'fail',
        message: 'requirements.txt is not satisfied for api',
        hint: 'run: python3 -m pip install -r /workspace/api/requirements.txt',
      },
    ]

    const { getByRole, getByText, queryByText } = render(HealthCheck)

    expect(getByText('1 issue needs attention')).toBeTruthy()
    expect(getByText('requirements.txt is not satisfied for api')).toBeTruthy()
    expect(getByText('python3 -m pip install -r /workspace/api/requirements.txt')).toBeTruthy()
    expect(queryByText('Python 3.13 is available')).toBeNull()
    expect(getByRole('button', { name: 'Show 1 other check' })).toBeTruthy()
  })

  it('reveals successful and informational checks on demand', async () => {
    store.daemon.doctorChecks = [
      { name: 'Config', status: 'info', message: '/workspace/orbit.yaml' },
      { name: 'Docker', status: 'pass', message: 'Docker is available' },
    ]

    const { getByRole, getByText, queryByText } = render(HealthCheck)

    expect(getByText('Environment is ready')).toBeTruthy()
    expect(queryByText('/workspace/orbit.yaml')).toBeNull()
    await fireEvent.click(getByRole('button', { name: 'Show 2 other checks' }))
    expect(getByText('/workspace/orbit.yaml')).toBeTruthy()
    expect(getByText('Docker is available')).toBeTruthy()
    expect(getByRole('button', { name: 'Hide 2 other checks' })).toBeTruthy()
  })

  it('copies an executable remedy instead of the hint prefix', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })
    store.daemon.doctorChecks = [
      {
        name: 'Packages (web)',
        status: 'fail',
        message: 'project packages are not installed',
        hint: 'run: pnpm --dir /workspace install',
      },
    ]

    const { getByRole } = render(HealthCheck)
    await fireEvent.click(getByRole('button', { name: 'Copy remedy command for Packages (web)' }))

    await waitFor(() => expect(writeText).toHaveBeenCalledWith('pnpm --dir /workspace install'))
  })

  it('shows non-command remediation as guidance', () => {
    store.daemon.doctorChecks = [
      {
        name: 'Docker',
        status: 'fail',
        message: 'Docker is unavailable',
        hint: 'Start Docker Desktop and run the checks again',
      },
    ]

    const { getByText, queryByRole } = render(HealthCheck)

    expect(getByText('How to fix it')).toBeTruthy()
    expect(getByText('Start Docker Desktop and run the checks again')).toBeTruthy()
    expect(queryByRole('button', { name: /Copy remedy/ })).toBeNull()
  })
})
