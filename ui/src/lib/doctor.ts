import { fetchDoctor } from './api'
import { store } from './stores.svelte'

const FRESH_WINDOW_MS = 30_000

let lastRunAt = 0

export async function runDoctorChecks(opts: { ifStale?: boolean } = {}) {
  if (store.daemon.doctorRunning) return
  if (opts.ifStale && Date.now() - lastRunAt < FRESH_WINDOW_MS && store.daemon.doctorChecks.length) return
  store.daemon.doctorRunning = true
  try {
    const res = await fetchDoctor()
    if (res) {
      store.daemon.doctorChecks = res.checks
      store.daemon.doctorRanAt = res.ran_at
      lastRunAt = Date.now()
    }
  } finally {
    store.daemon.doctorRunning = false
  }
}
