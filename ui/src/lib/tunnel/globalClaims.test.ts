import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchGlobalClaims } from './api'

describe('fetchGlobalClaims', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('returns claims on success', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        claims: [
          { path_prefix: '/callbacks/x', owner: 'logan', expires_at: '2026-06-08T14:27:00Z', mine: true },
        ],
      }),
    })))
    const claims = await fetchGlobalClaims()
    expect(claims).toHaveLength(1)
    expect(claims[0].mine).toBe(true)
  })

  it('throws the daemon error message on failure (not swallowed)', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: false,
      status: 502,
      json: async () => ({ error: 'cannot reach Tunlease gateway' }),
    })))
    await expect(fetchGlobalClaims()).rejects.toThrow(/Tunlease gateway/)
  })

  it('tolerates a missing claims array', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({}) })))
    expect(await fetchGlobalClaims()).toEqual([])
  })
})
