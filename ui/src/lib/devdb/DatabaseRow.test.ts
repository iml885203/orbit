import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import DatabaseOperationsList from './DatabaseOperationsList.svelte'
import DatabaseRow from './DatabaseRow.svelte'
import type { DBOpInFlight } from './stores.svelte'

const project = { name: 'Wallet', path: '/db/wallet', databases: ['WalletDB'] }

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('DatabaseRow reset flow', () => {
  it('opens one destructive confirmation and acknowledges data loss', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ok: true }) })
    vi.stubGlobal('fetch', fetchMock)
    render(DatabaseOperationsList, { props: { project, states: {}, operation: null } })

    await fireEvent.click(screen.getByRole('button', { name: 'Reset…' }))
    expect(screen.getByRole('dialog', { name: 'Reset WalletDB?' })).toBeInTheDocument()
    expect(screen.getByText('This disconnects database clients, discards local data, and applies the latest schema. This cannot be undone.')).toBeInTheDocument()
    await fireEvent.click(screen.getByRole('button', { name: 'Reset database' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/db/reset', expect.objectContaining({ body: JSON.stringify({ db: 'WalletDB', acknowledgeDataLoss: true }) })))
  })

  it('offers Publish anyway after a data-loss block and posts a forced publish', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ok: true }) })
    vi.stubGlobal('fetch', fetchMock)
    const operation: DBOpInFlight = { op: 'publish', db: 'WalletDB', all: false, startedAt: 't', lines: [], done: true, ok: false, err: 'blocked', errorCode: 'publish_blocked_data_loss' }
    render(DatabaseOperationsList, { props: { project, states: {}, operation } })

    await fireEvent.click(screen.getByRole('button', { name: /Publish anyway…/ }))
    expect(screen.getByRole('dialog', { name: 'Publish WalletDB despite data loss?' })).toBeInTheDocument()
    await fireEvent.click(screen.getByRole('button', { name: 'Publish anyway' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/db/publish', expect.objectContaining({ body: JSON.stringify({ db: 'WalletDB', force: true }) })))
  })

  it('confirms an analyzed data-loss diff before the first publish attempt', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ok: true }) })
    vi.stubGlobal('fetch', fetchMock)
    render(DatabaseOperationsList, {
      props: {
        project,
        states: {},
        operation: null,
        driftByDB: { WalletDB: { inSync: false, changes: 2, dataLoss: true } },
      },
    })

    await fireEvent.click(screen.getByRole('button', { name: 'Publish…' }))

    expect(fetchMock).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog', { name: 'Publish WalletDB despite data loss?' })).toBeInTheDocument()
    expect(screen.getByText(/analyzed schema changes may discard data/)).toBeInTheDocument()

    await fireEvent.click(screen.getByRole('button', { name: 'Publish anyway' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/db/publish', expect.objectContaining({ body: JSON.stringify({ db: 'WalletDB', force: true }) })))
  })

  it('does not publish when the analyzed data-loss confirmation is cancelled', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ok: true }) })
    vi.stubGlobal('fetch', fetchMock)
    render(DatabaseOperationsList, {
      props: {
        project,
        states: {},
        operation: null,
        driftByDB: { WalletDB: { inSync: false, changes: 1, dataLoss: true } },
      },
    })

    await fireEvent.click(screen.getByRole('button', { name: 'Publish…' }))
    await fireEvent.click(screen.getByText('Cancel', { selector: 'button' }))

    expect(screen.queryByRole('dialog', { name: /despite data loss/ })).toBeNull()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('does not trust a stale data-loss result when publishing', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ok: true }) })
    vi.stubGlobal('fetch', fetchMock)
    render(DatabaseOperationsList, {
      props: {
        project,
        states: {},
        operation: null,
        driftByDB: { WalletDB: { inSync: false, changes: 2, dataLoss: true, stale: true } },
      },
    })

    await fireEvent.click(screen.getByRole('button', { name: 'Publish' }))

    expect(screen.queryByRole('dialog', { name: /despite data loss/ })).toBeNull()
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/db/publish', expect.objectContaining({ body: JSON.stringify({ db: 'WalletDB', force: false }) })))
  })

  it('keeps the plain failure text for non-data-loss publish failures', () => {
    const operation: DBOpInFlight = { op: 'publish', db: 'WalletDB', all: false, startedAt: 't', lines: [], done: true, ok: false, err: 'build failed', errorCode: 'build_failed' }
    render(DatabaseOperationsList, { props: { project, states: {}, operation } })
    expect(screen.queryByRole('button', { name: /Publish anyway/ })).toBeNull()
    expect(screen.getByRole('alert')).toHaveTextContent('build failed')
  })

  it('shows reset-specific running status and progress', () => {
    const operation: DBOpInFlight = { op: 'reset', db: 'WalletDB', startedAt: new Date().toISOString(), lines: [], done: false, ok: false }
    render(DatabaseRow, { props: { database: 'WalletDB', operation, elapsedSeconds: 7, onPublish: vi.fn(), onReset: vi.fn(), onViewLog: vi.fn(), onDiff: vi.fn() } })

    expect(screen.getByRole('status')).toHaveTextContent('Resetting')
    expect(screen.getByText('Resetting… 7s')).toBeInTheDocument()
  })

  it('disables reset and marks not-published for a database that does not exist', () => {
    render(DatabaseRow, { props: { database: 'WalletDB', resetState: { exists: false, hasBaseline: false }, onPublish: vi.fn(), onReset: vi.fn(), onViewLog: vi.fn(), onDiff: vi.fn() } })

    expect(screen.getByRole('button', { name: 'Reset…' })).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent('Not published')
    expect(screen.getByText('Publish creates this database.')).toBeInTheDocument()
  })

  it('explains and confirms the first reset as a database recreation', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ok: true }) })
    vi.stubGlobal('fetch', fetchMock)
    render(DatabaseOperationsList, {
      props: {
        project,
        states: {},
        resetStates: { WalletDB: { exists: true, hasBaseline: false } },
        operation: null,
      },
    })

    expect(screen.getByText('No reset point yet.')).toBeInTheDocument()
    await fireEvent.click(screen.getByRole('button', { name: 'Reset…' }))

    expect(screen.getByRole('dialog', { name: 'Reset WalletDB by recreating it?' })).toBeInTheDocument()
    expect(screen.getByText(/save a reset point for next time/)).toBeInTheDocument()
    await fireEvent.click(screen.getByRole('button', { name: 'Recreate database' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/db/reset', expect.objectContaining({ body: JSON.stringify({ db: 'WalletDB', acknowledgeDataLoss: true }) })))
  })

})
