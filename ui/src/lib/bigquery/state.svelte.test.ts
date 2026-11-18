import { afterEach, describe, expect, it, vi } from 'vitest'
import { getJSON } from '$lib/api'
import { bigQueryHidden, bqState, refreshBigQuery, resetBigQuery } from './state.svelte'
import type { BQSnapshot } from '$lib/types/bigquery.gen'
vi.mock('$lib/api', () => ({ getJSON: vi.fn() }))
const snapshot: BQSnapshot = { configured: true, profiles: [], records: [], source: 'local', observed_at: 'now' }
describe('BigQuery metadata', () => {
  afterEach(() => { resetBigQuery(); vi.clearAllMocks() })
  it('does not let a response from the previous environment restore its profiles', async () => {
    let resolve!: (value: BQSnapshot) => void
    vi.mocked(getJSON).mockImplementationOnce(() => new Promise(r => { resolve = r }))
    const pending = refreshBigQuery()
    resetBigQuery()
    resolve(snapshot)
    await pending
    expect(bqState.snapshot).toBeNull()
    expect(bigQueryHidden()).toBe(true)
  })
  it('preserves the last snapshot on failure and marks it stale', async () => {
    vi.mocked(getJSON).mockResolvedValueOnce(snapshot).mockResolvedValueOnce(null)
    await refreshBigQuery()
    expect(bigQueryHidden()).toBe(false)
    await refreshBigQuery()
    expect(bqState.snapshot).toEqual(snapshot)
    expect(bqState.reachable).toBe(false)
  })
})
