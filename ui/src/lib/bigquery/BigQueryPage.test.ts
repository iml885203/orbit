import { render, screen, fireEvent } from '@testing-library/svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import BigQueryPage from './BigQueryPage.svelte'
import { resetBigQuery } from './state.svelte'

beforeEach(() => {
  vi.spyOn(globalThis, 'fetch')
  vi.spyOn(console, 'error').mockImplementation(() => {})
})

describe('BigQueryPage', () => {
  afterEach(() => { resetBigQuery(); vi.restoreAllMocks() })
  it('guards an unconfigured direct route', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify({ configured: false, profiles: [], records: [], source: 'local', observed_at: '2026-11-18T00:00:00Z' })))
    render(BigQueryPage)
    expect(await screen.findByText(/no BigQuery configuration/)).toHaveAttribute('role', 'status')
    expect(screen.queryByText('Sandbox records')).not.toBeInTheDocument()
  })
  it('distinguishes failed initial fetch from an empty sandbox list', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 503 }))
    render(BigQueryPage)
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not load')
    expect(screen.queryByText(/No local sandbox records/)).not.toBeInTheDocument()
  })
  it('keeps provenance and stale state visible', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify({ configured: true, profiles: [{ name: 'integration', mode: 'sandbox', job_project: 'jobs-project', data_project: 'data-project', location: 'US' }], records: [], source: 'local', observed_at: '2026-11-18T00:00:00Z' })))
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 503 }))
    render(BigQueryPage)
    await screen.findByText(/No local sandbox records/)
    await fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('last local snapshot')
    expect(screen.getByText(/application readiness are unverified/)).toBeInTheDocument()
    expect(screen.getByText(/No local sandbox records/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /prepare|clean/i })).not.toBeInTheDocument()
  })
  it('does not turn unreadable recovery evidence into an empty list or prepare advice', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify({ configured: true, profiles: [], records: [], source: 'local', observed_at: 'now', error: 'Local recovery records could not be read.' })))
    render(BigQueryPage)
    expect(await screen.findByRole('alert')).toHaveTextContent('could not be read')
    expect(screen.getByRole('status')).toHaveTextContent('records are unavailable')
    expect(screen.queryByText(/No local sandbox records/)).not.toBeInTheDocument()
    expect(screen.queryByText(/orbit bq sandbox prepare/)).not.toBeInTheDocument()
  })

})
