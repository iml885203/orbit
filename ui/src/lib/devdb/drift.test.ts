import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({ diffDB: vi.fn(), fetchDriftSnapshot: vi.fn(async () => null) }))
import { diffDB, fetchDriftSnapshot } from './api'
import { createDriftController, summarizeProject } from './drift.svelte'
import type { DriftSummary } from './drift.svelte'
import type { DiffResult } from '$lib/types.gen'

const sync: DriftSummary = { inSync: true, changes: 0, dataLoss: false }
const changed = (n: number, dataLoss = false): DriftSummary => ({ inSync: false, changes: n, dataLoss })

describe('summarizeProject', () => {
  it('is undefined when no database has been diffed', () => {
    expect(summarizeProject(['A', 'B'], {})).toBeUndefined()
  })

  it('rolls up to in-sync when every diffed DB is in sync', () => {
    expect(summarizeProject(['A', 'B'], { A: sync, B: sync })).toEqual({ kind: 'in-sync' })
  })

  it('sums changes across databases and carries data-loss', () => {
    const pd = summarizeProject(['A', 'B'], { A: changed(2), B: changed(3, true) })
    expect(pd).toEqual({ kind: 'changes', changes: 5, dataLoss: true, stale: false })
  })

  it('reports in-sync even if some DBs are undiffed, as long as the diffed ones are synced', () => {
    // C undiffed; A synced → the seen set is all in sync.
    expect(summarizeProject(['A', 'B', 'C'], { A: sync })).toEqual({ kind: 'in-sync' })
  })

  it('an error on any diffed DB dominates the badge', () => {
    const byDB = { A: changed(2), B: { inSync: false, changes: 0, dataLoss: false, error: 'boom' } }
    expect(summarizeProject(['A', 'B'], byDB)).toEqual({ kind: 'error' })
  })

  it('marks the rollup stale when any diffed DB summary is stale', () => {
    const pd = summarizeProject(['A', 'B'], { A: { ...changed(2), stale: true }, B: sync })
    expect(pd).toEqual({ kind: 'changes', changes: 2, dataLoss: false, stale: true })
  })

  it('shows unchecked when a database needs its first engine diff', () => {
    const pd = summarizeProject(['A'], { A: { inSync: false, changes: 0, dataLoss: false, needsEngine: true } })
    expect(pd).toEqual({ kind: 'unchecked' })
  })
})

const mockDiff = vi.mocked(diffDB)
const inSyncResult = (db: string): DiffResult =>
  ({ db, in_sync: true, created: 0, altered: 0, dropped: 0, data_loss: false, ops: [], alerts: [] })
const okResponse = (db: string) => ({ ok: true, data: { result: inSyncResult(db) } })

describe('checkAll', () => {
  beforeEach(() => { mockDiff.mockReset() })

  it('updates badges and progress incrementally as each diff lands', async () => {
    // Deferred per-DB resolvers so the test controls completion order.
    const resolvers = new Map<string, () => void>()
    mockDiff.mockImplementation((db: string) =>
      new Promise((res) => resolvers.set(db, () => res(okResponse(db)))))
    const c = createDriftController()
    const sweep = c.checkAll(['A', 'B'])
    await vi.waitFor(() => expect(resolvers.size).toBe(2))
    expect(c.checkingAll).toBe(true)
    expect(c.checkProgress).toEqual({ done: 0, total: 2 })

    resolvers.get('B')!()
    await vi.waitFor(() => expect(c.checkProgress.done).toBe(1))
    // B's badge is live while A is still in flight.
    expect(c.byDB.B).toEqual({ inSync: true, changes: 0, dataLoss: false })
    expect(c.byDB.A).toBeUndefined()
    expect(c.checkingAll).toBe(true)

    resolvers.get('A')!()
    await sweep
    expect(c.checkProgress).toEqual({ done: 2, total: 2 })
    expect(c.checkingAll).toBe(false)
    expect(mockDiff).toHaveBeenCalledWith('A', false, 'normal')
    expect(mockDiff).toHaveBeenCalledWith('B', false, 'normal')
  })

  it('uses fast-only checks for automatic page refresh and marks cache misses unchecked', async () => {
    mockDiff.mockImplementation(async (db: string) =>
      db === 'New' ? { ok: true, data: { needs_engine: true } } : okResponse(db))
    const c = createDriftController()
    await c.checkFast(['Known', 'New'])
    expect(c.byDB.Known).toEqual({ inSync: true, changes: 0, dataLoss: false })
    expect(c.byDB.New).toEqual({ inSync: false, changes: 0, dataLoss: false, needsEngine: true })
    expect(mockDiff).toHaveBeenCalledWith('Known', false, 'fast')
    expect(mockDiff).toHaveBeenCalledWith('New', false, 'fast')
  })

  it('warms first-time databases automatically after fast results land', async () => {
    mockDiff.mockImplementation(async (db: string, _script?: boolean, mode?: string) =>
      db === 'New' && mode === 'fast' ? { ok: true, data: { needs_engine: true } } : okResponse(db))
    const c = createDriftController()

    await c.checkOnEntry(['Known', 'New'])

    expect(c.byDB.Known).toEqual({ inSync: true, changes: 0, dataLoss: false })
    expect(c.byDB.New).toEqual({ inSync: true, changes: 0, dataLoss: false })
    expect(mockDiff).toHaveBeenNthCalledWith(1, 'Known', false, 'fast')
    expect(mockDiff).toHaveBeenNthCalledWith(2, 'New', false, 'fast')
    expect(mockDiff).toHaveBeenNthCalledWith(3, 'New', false, 'normal')
  })

  it('keeps at most 4 diffs in flight', async () => {
    let inFlight = 0
    let peak = 0
    const resolvers: Array<() => void> = []
    mockDiff.mockImplementation((db: string) => {
      inFlight++
      peak = Math.max(peak, inFlight)
      return new Promise((res) => resolvers.push(() => { inFlight--; res(okResponse(db)) }))
    })
    const c = createDriftController()
    const sweep = c.checkAll(['A', 'B', 'C', 'D', 'E', 'F'])
    await vi.waitFor(() => expect(mockDiff).toHaveBeenCalledTimes(4))
    while (resolvers.length || inFlight) {
      resolvers.shift()?.()
      await Promise.resolve()
      await Promise.resolve()
    }
    await sweep
    expect(peak).toBe(4)
    expect(mockDiff).toHaveBeenCalledTimes(6)
  })

  it('records a per-DB failure as an error badge and finishes the sweep', async () => {
    mockDiff.mockImplementation(async (db: string) =>
      db === 'Bad' ? { ok: false, data: { error: 'boom' } } : okResponse(db))
    const c = createDriftController()
    await c.checkAll(['Good', 'Bad'])
    expect(c.byDB.Good).toEqual({ inSync: true, changes: 0, dataLoss: false })
    expect(c.byDB.Bad).toEqual({ inSync: false, changes: 0, dataLoss: false, error: 'boom' })
  })

  it('seeds badges from the daemon drift cache without clobbering live ones', async () => {
    mockDiff.mockImplementation(async (db: string) => okResponse(db))
    vi.mocked(fetchDriftSnapshot).mockResolvedValue({
      entries: [
        { db: 'A', result: { ...inSyncResult('A'), in_sync: false, created: 2 }, at: 't', stale: true },
        { db: 'B', error: 'boom', at: 't' },
        { db: 'C', result: inSyncResult('C'), at: 't' },
      ],
    })
    const c = createDriftController()
    await c.refreshDrift('C') // live badge this session — load must not overwrite it
    await c.load()
    expect(c.byDB.A).toEqual({ inSync: false, changes: 2, dataLoss: false, stale: true })
    expect(c.byDB.B).toEqual({ inSync: false, changes: 0, dataLoss: false, error: 'boom' })
    expect(c.byDB.C).toEqual({ inSync: true, changes: 0, dataLoss: false })
  })

  it('prunes databases that left the project list', async () => {
    mockDiff.mockImplementation(async (db: string) => okResponse(db))
    const c = createDriftController()
    await c.checkAll(['A', 'B'])
    expect(Object.keys(c.byDB).sort()).toEqual(['A', 'B'])
    await c.checkAll(['A'])
    expect(Object.keys(c.byDB)).toEqual(['A'])
  })
})
