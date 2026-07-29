import { store, toast } from './stores.svelte'
import { detachEdge } from './api'
import type { GraphEdge, GraphNode } from './types.gen'

export type EnvironmentPrimaryAction =
  | { kind: 'start'; label: string; resource?: string }
  | { kind: 'logs'; label: string; resource: string }
  | { kind: 'inspect'; label: string; resource: string }
  | { kind: 'open'; label: string; url: string }
  | { kind: 'busy'; label: string }
  | null

export type EnvironmentActionState = {
  summary: string
  primary: EnvironmentPrimaryAction
}

export function environmentActionState(nodes: GraphNode[]): EnvironmentActionState {
  const resources = nodes.filter(node => node.kind !== 'external')
  const healthy = resources.filter(node => node.state === 'healthy').length
  const stopped = resources.filter(node => node.state === 'stopped' || node.state === 'pending').length
  const changing = resources.filter(node =>
    ['starting', 'building', 'stopping', 'restarting'].includes(node.state)
  ).length
  const problem = resources.find(node =>
    node.state === 'degraded' || node.state === 'stopped' || node.state === 'pending'
  )
  const issue = problem ? rootCause(problem, resources) : null
  const app =
    resources.find(node => node.kind === 'frontend' && node.state === 'healthy' && node.url) ??
    resources.find(node => node.state === 'healthy' && node.url) ??
    null

  let summary = 'No resources configured'
  if (resources.length > 0 && changing > 0) {
    summary = `${changing} changing · ${healthy} healthy`
  } else if (resources.length > 0 && healthy === resources.length) {
    summary = `${healthy} healthy`
  } else if (resources.length > 0 && stopped === resources.length) {
    summary = `${stopped} stopped`
  } else if (resources.length > 0) {
    const degraded = resources.length - healthy - stopped
    summary = [
      healthy ? `${healthy} healthy` : '',
      degraded ? `${degraded} need attention` : '',
      stopped ? `${stopped} stopped` : '',
    ].filter(Boolean).join(' · ')
  }

  if (changing > 0) {
    return { summary, primary: { kind: 'busy', label: 'Environment changing…' } }
  }
  if (issue?.state === 'degraded') {
    const primary: EnvironmentPrimaryAction = issue.logsAvailable
      ? { kind: 'logs', label: `View ${issue.name} logs`, resource: issue.name }
      : { kind: 'inspect', label: `Inspect ${issue.name}`, resource: issue.name }
    return { summary, primary }
  }
  if (issue?.state === 'stopped' || issue?.state === 'pending') {
    const primary: EnvironmentPrimaryAction = stopped === resources.length
      ? { kind: 'start', label: 'Start environment' }
      : { kind: 'start', label: `Start ${issue.name}`, resource: issue.name }
    return { summary, primary }
  }
  if (app?.url) {
    return { summary, primary: { kind: 'open', label: `Open ${app.name}`, url: app.url } }
  }
  return { summary, primary: null }
}

function rootCause(start: GraphNode, nodes: GraphNode[]): GraphNode {
  const seen = new Set([start.name])
  let current = start
  while (current.blockedBy && !seen.has(current.blockedBy)) {
    seen.add(current.blockedBy)
    const next = nodes.find(candidate => candidate.name === current.blockedBy)
    if (!next) break
    current = next
  }
  return current
}

/**
 * Flip an edge's detached state with optimistic UI: mutate store.graph.data
 * immediately, then call the API; revert on failure. Returns true on
 * success, false on failure (toast already shown).
 */
export async function optimisticDetach(edge: GraphEdge, detached: boolean): Promise<boolean> {
  const env = store.graph.data?.env ?? ''
  const apply = (target: boolean) => {
    if (!store.graph.data) return
    const idx = store.graph.data.edges.findIndex(e => e.from === edge.from && e.to === edge.to)
    if (idx >= 0) store.graph.data.edges[idx] = { ...edge, detached: target }
  }
  apply(detached)
  const { ok, data } = await detachEdge(env, edge.from, edge.to, detached)
  if (!ok) {
    apply(!detached)
    toast(data?.error || 'Failed to update edge')
    return false
  }
  toast(data?.message || (detached ? 'Detached' : 'Reattached'))
  return true
}
