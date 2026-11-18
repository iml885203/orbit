import { getJSON } from '$lib/api'
import type { BQSnapshot } from '$lib/types/bigquery.gen'

export const bqState = $state({ snapshot: null as BQSnapshot | null, reachable: true })
let epoch = 0
let request = 0

export async function refreshBigQuery() {
  const currentEpoch = epoch
  const currentRequest = ++request
  const result = await getJSON<BQSnapshot>('/api/features/bigquery')
  if (currentEpoch !== epoch || currentRequest !== request) return
  bqState.reachable = result !== null
  if (result) bqState.snapshot = result
}

export function resetBigQuery() {
  epoch++
  bqState.snapshot = null
  bqState.reachable = true
}

export function bigQueryHidden() {
  return bqState.snapshot?.configured !== true
}
