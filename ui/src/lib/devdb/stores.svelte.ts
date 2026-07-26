// DB-workflow UI state — holds everything the DB workflow and tunnel
// pages render. Owned here so the core store declares no feature state.
import type { DevDBMetaResponse, DevDBProject, DBState } from '$lib/types.gen'

// DBOpInFlight mirrors the dbop SSE frames' in-flight state (moved
// verbatim from the core store).
export type DBOpInFlight = {
	op: 'publish' | 'reset'
	// all: publish spans every database; db is then empty.
	all?: boolean
	db: string
	startedAt: string
	lines: string[]
	done: boolean
	ok: boolean
	err?: string
	errorCode?: string
	durationMs?: number
}

class DevStore {
	devMeta = $state<DevDBMetaResponse | null>(null)
	dbProjects = $state<DevDBProject[]>([])
	dbState = $state<Record<string, DBState>>({})
	dbOpInFlight = $state<DBOpInFlight | null>(null)
}

export const devStore = new DevStore()

// dbWorkflowHidden hides the DB surfaces when the daemon reports the
// active env has no sql-server container. Fail-open: unknown (meta not
// loaded yet, or an older daemon without the field) counts as configured,
// so users never see the DB UI flash out. Only an explicit false hides.
export function dbWorkflowHidden(): boolean {
	return devStore.devMeta?.db_configured === false
}

// tunnelHidden hides the Tunnels tab when the active env has no claim
// section (no tunnel support). Same fail-open rule as dbWorkflowHidden:
// only an explicit false hides, so the tab never wrongly vanishes while
// meta is loading or on an older daemon.
export function tunnelHidden(): boolean {
	return devStore.devMeta?.claim_configured === false
}

// resetDevState clears env-derived feature state when a new daemon epoch
// is detected — called from the ext module's onDaemonReset hook.
export function resetDevState() {
	devStore.devMeta = null
	devStore.dbProjects = []
	devStore.dbState = {}
	devStore.dbOpInFlight = null
}
