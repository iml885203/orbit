import { subscribe } from '$lib/eventbus'
import { toast } from '$lib/stores.svelte'
import { devStore } from './stores.svelte'
import { dbOpLabel } from './dbOpView.svelte'
import { dbOpHint } from './dbOpHints'
import type { DBOpFrame } from '$lib/types.gen'

export function startDBOpStream(): () => void {
  return subscribe('dbop', (data) => {
    const frame = data as DBOpFrame
    switch (frame.kind) {
      case 'idle':
        // No active op. If we had one before, leave its final state
        // so the user can read the panel until they close it. The
        // panel's own Close button clears dbOpInFlight.
        break
      case 'start':
        devStore.dbOpInFlight = {
          op: frame.op as 'publish' | 'reset',
          all: frame.all ?? false,
          db: frame.db ?? '',
          startedAt: frame.startedAt!,
          lines: [],
          done: false,
          ok: false,
        }
        break
      case 'output':
        if (devStore.dbOpInFlight) {
          devStore.dbOpInFlight = {
            ...devStore.dbOpInFlight,
            lines: [...devStore.dbOpInFlight.lines, frame.line ?? ''],
          }
        }
        break
      case 'done':
        if (devStore.dbOpInFlight) {
          devStore.dbOpInFlight = {
            ...devStore.dbOpInFlight,
            done: true,
            ok: frame.ok ?? false,
            err: frame.err,
            errorCode: frame.errorCode,
            durationMs: frame.durationMs,
          }
          const opLabel = devStore.dbOpInFlight.op
          const db = dbOpLabel(devStore.dbOpInFlight)
          if (frame.ok) {
            const seconds = ((frame.durationMs ?? 0) / 1000).toFixed(1)
            toast(`${devStore.dbOpInFlight.op === 'reset' ? 'Reset' : 'Published'} ${db} in ${seconds}s`)
          } else {
            // Turn the raw failure into the next action when we recognize it.
            const hint = dbOpHint(opLabel, devStore.dbOpInFlight.lines, frame.errorCode)
            toast(hint
              ? `${opLabel} ${db} failed — ${hint}`
              : `${opLabel} ${db} failed: ${frame.err ?? 'error'}`)
          }
        }
        break
    }
  })
}
