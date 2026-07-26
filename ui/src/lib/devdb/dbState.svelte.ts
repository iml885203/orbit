import { subscribe } from '$lib/eventbus'
import { devStore } from './stores.svelte'
import type { Snapshot as DBStateSnapshot } from '$lib/types.gen'

export function startDBStateStream(): () => void {
  return subscribe('dbstate', (data) => {
    const snap = data as DBStateSnapshot
    devStore.dbState = snap.dbs ?? {}
  })
}
