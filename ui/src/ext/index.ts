// The extension UI module — the dashboard counterpart of the Go-side
// branded root (cmd/orbit's Extensions() in extensions.go). The core
// imports this through the $ext vite alias; an out-of-tree overlay build
// could point ORBIT_UI_EXT at its own entry. This is the ExampleTeam feature
// wiring; the neutral pages it mounts live in $lib/devdb and $lib/tunnel.
import type { Component } from 'svelte'
import type { RouteDefinition } from 'svelte-spa-router'
import DevDBPage from '$lib/devdb/DevDBPage.svelte'
import TunnelPage from '$lib/tunnel/TunnelPage.svelte'
import LocalDatabasesPanel from '$lib/devdb/LocalDatabasesPanel.svelte'
import { startDBStateStream } from '$lib/devdb/dbState.svelte'
import { startDBOpStream } from '$lib/devdb/dbOps.svelte'
import { startTunnelAccessStream } from '$lib/tunnel/tunnelAccess.svelte'
import { dbWorkflowHidden, tunnelHidden, resetDevState, devStore } from '$lib/devdb/stores.svelte'
import { refreshDevMeta } from '$lib/devdb/api'
import type { SettingsWire } from '$lib/devdb/api'

export type ExtNavItem = {
  path: string
  label: string
  hidden?: () => boolean
}

// Nav tabs and routes the core registry spreads after its own.
export const navItems: ExtNavItem[] = [
  { path: '/devdb', label: 'SQL Server', hidden: dbWorkflowHidden },
  { path: '/tunnel', label: 'Tunnels', hidden: tunnelHidden },
]

export const routes: RouteDefinition = {
  '/devdb': DevDBPage,
  '/tunnel': TunnelPage,
}

// onAppMount starts the feature SSE streams; returns the combined
// cleanup. Mirrors the daemon's DaemonSetup: construction happens here,
// the core only invokes.
export function onAppMount(): () => void {
  const cleanups = [startDBStateStream(), startDBOpStream(), startTunnelAccessStream()]
  // Early devMeta fetch, in parallel with the core's settings fetch:
  // shrinks the window where db_configured is unknown (the nav tab and
  // DevDB page defer to it, fail-open shows the tab).
  void refreshDevMeta({ quiet: true })
  return () => cleanups.forEach((fn) => fn())
}

// onDaemonState runs whenever the core refetches daemon-derived state
// (startup and daemon-epoch changes). The overlay currently derives no
// state from raw settings — the SQL image is a deploy-time env config,
// not a runtime toggle — but the seam stays for future needs.
export function onDaemonState(_settings: SettingsWire) {}

// onEnvChanged keeps the DB-workflow gate in sync when the live graph
// lands on a different env (a switch can add/remove the sqlserver section).
export function onEnvChanged() {
  void refreshDevMeta({ quiet: true })
}

// onDaemonReset clears env-derived feature state on a new daemon epoch,
// then refetches the DB-workflow meta in parallel with the core's
// daemon-state refetch (same early-fetch rationale as onAppMount).
export function onDaemonReset() {
  resetDevState()
  void refreshDevMeta({ quiet: true })
}

// nodeDrawerPanels render inside the graph node drawer for matching
// nodes — the seam that keeps feature panels out of the core drawer.
export const nodeDrawerPanels: Array<{
  match: (nodeName: string) => boolean
  title: string
  key: string
  component: Component
}> = [
  {
    match: (name) => name === devStore.devMeta?.sql_server_service,
    title: 'Database Projects',
    key: 'databaseProjects',
    component: LocalDatabasesPanel,
  },
]

// settingsSections render inside the settings popover after the core rows.
export const settingsSections: Component[] = []
