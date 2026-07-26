import type { DBOpInFlight } from './stores.svelte'

export type SceneState = 'idle' | 'ready' | 'building' | 'complete' | 'failed'

export function deriveSceneState(
  dbOpInFlight: DBOpInFlight | null,
  sqlServerHealthy: boolean,
  projectSelected: boolean,
): SceneState {
  if (!projectSelected) return 'idle'
  if (dbOpInFlight && !dbOpInFlight.done) return 'building'
  if (dbOpInFlight?.done) return dbOpInFlight.ok ? 'complete' : 'failed'
  return sqlServerHealthy ? 'ready' : 'idle'
}

export function publishProgressPercent(dbOpInFlight: DBOpInFlight | null, elapsedSeconds: number): number {
  if (dbOpInFlight?.done) return dbOpInFlight.ok ? 100 : 0
  if (!dbOpInFlight) return 0
  return 100 * (1 - Math.exp(-Math.max(0, elapsedSeconds) / 8))
}
