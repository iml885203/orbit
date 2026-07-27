// View helpers shared by the two publish surfaces (the SQL Server page and
// the target drawer's operations list): a ticking elapsed-seconds
// counter, the disabled-reason string, and the log-modal label. Homed
// here so the page and drawer can't drift on the same publish op.

import { store } from '$lib/stores.svelte'
import { devStore, type DBOpInFlight } from './stores.svelte'

// dbOpRunning reports whether a publish op is in flight (not yet done).
export function dbOpRunning(op: DBOpInFlight | null): boolean {
	return !!op && !op.done
}

// dbOpLabel names an operation for the log modal / toasts — "all
// databases" for an --all run, else the single database. One owner of
// the all-vs-db wording.
export function dbOpLabel(op: DBOpInFlight): string {
	return op.all ? 'all databases' : op.db
}

// publishDisabledReason is the shared gate both surfaces show on their
// publish controls: the configured target must be healthy and no op already in
// flight. Empty string means enabled.
export function publishDisabledReason(): string {
	const target = devStore.devMeta?.sql_server_service
	if (!target) {
		return 'SQL Server target is unavailable in the active environment'
	}
	if (store.daemon.services[target]?.state !== 'healthy') {
		return `Start ${target} before changing databases`
	}
	if (dbOpRunning(devStore.dbOpInFlight)) {
		return 'Another database operation is in progress'
	}
	return ''
}

// createElapsed returns a live elapsed-seconds counter for an in-flight
// op: it ticks once a second while running and reads 0 otherwise. Must
// be called during component init (it registers an $effect).
export function createElapsed(op: () => DBOpInFlight | null) {
	let tick = $state(Date.now())
	$effect(() => {
		if (!dbOpRunning(op())) return
		tick = Date.now()
		const timer = window.setInterval(() => (tick = Date.now()), 1000)
		return () => window.clearInterval(timer)
	})
	return {
		get seconds() {
			const current = op()
			return dbOpRunning(current) && current
				? Math.max(0, Math.floor((tick - Date.parse(current.startedAt)) / 1000))
				: 0
		},
	}
}

// createSubmitGuard closes the window between a publish click and the
// op's SSE start frame: begin() claims the slot (false if already
// pending or running), the $effect releases it once the daemon confirms
// the op is running, and reset() releases it if the request failed.
// Shared so the page (Publish all) and the operations list (per-DB
// Publish) guard identically.
export function createSubmitGuard(running: () => boolean) {
	let submitting = $state(false)
	let verb = $state<'publish' | 'reset'>('publish')
	$effect(() => {
		if (running()) submitting = false
	})
	return {
		get active() {
			return submitting
		},
		// The verb being submitted (typed), or null when idle — so
		// surfaces label the right control without parsing `reason`.
		get pendingVerb(): 'publish' | 'reset' | null {
			return submitting ? verb : null
		},
		// The disabled-reason both surfaces show while the POST is in
		// flight — owned here (verb included) so the page and drawer
		// never drift on the wording.
		get reason() {
			return submitting ? `Starting ${verb}…` : ''
		},
		begin(which: 'publish' | 'reset' = 'publish') {
			if (submitting || running()) return false
			verb = which
			submitting = true
			return true
		},
		reset() {
			submitting = false
		},
	}
}
