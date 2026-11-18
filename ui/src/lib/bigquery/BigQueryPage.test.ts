import { render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import BigQueryPage from './BigQueryPage.svelte'
import { bqState, resetBigQuery } from './state.svelte'

vi.mock('$lib/api', () => ({ getJSON: vi.fn(() => new Promise(() => {})) }))

describe('BigQueryPage', () => {
  afterEach(() => resetBigQuery())
  it('guards an unconfigured direct route', () => {
    bqState.snapshot = { configured: false, profiles: [], records: [], source: 'local', observed_at: '2026-11-18T00:00:00Z' }
    render(BigQueryPage)
    expect(screen.getByRole('status')).toHaveTextContent('no BigQuery configuration')
    expect(screen.queryByText('Sandbox records')).not.toBeInTheDocument()
  })
  it('distinguishes failed initial fetch from an empty sandbox list', () => {
    bqState.reachable = false
    render(BigQueryPage)
    expect(screen.getByRole('alert')).toHaveTextContent('Could not load')
    expect(screen.queryByText(/No local sandbox records/)).not.toBeInTheDocument()
  })
  it('keeps provenance and stale state visible', () => {
    bqState.snapshot = { configured: true, profiles: [{ name: 'integration', mode: 'sandbox', job_project: 'jobs-project', data_project: 'data-project', location: 'US' }], records: [], source: 'local', observed_at: '2026-11-18T00:00:00Z' }
    bqState.reachable = false
    render(BigQueryPage)
    expect(screen.getByRole('alert')).toHaveTextContent('last local snapshot')
    expect(screen.getByText(/application readiness are unverified/)).toBeInTheDocument()
    expect(screen.getByText(/No local sandbox records/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /prepare|clean/i })).not.toBeInTheDocument()
  })
  it('does not turn unreadable recovery evidence into an empty list or prepare advice', () => {
    bqState.snapshot = { configured: true, profiles: [], records: [], source: 'local', observed_at: 'now', error: 'Local recovery records could not be read.' }
    render(BigQueryPage)
    expect(screen.getByRole('alert')).toHaveTextContent('could not be read')
    expect(screen.getByRole('status')).toHaveTextContent('records are unavailable')
    expect(screen.queryByText(/No local sandbox records/)).not.toBeInTheDocument()
    expect(screen.queryByText(/orbit bq sandbox prepare/)).not.toBeInTheDocument()
  })

})
