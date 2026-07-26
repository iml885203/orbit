// Service-kind → identity colour, shared by the tracing table and waterfall.
// Kind carries identity (frontend / backend / infra), never health — see
// DESIGN.md and the global acceptance criteria.

import type { GraphNode } from './types.gen'

export function kindColorVar(kind: string | undefined): string {
  switch (kind) {
    case 'frontend': return 'var(--kind-frontend)'
    case 'infra': return 'var(--kind-infra)'
    default: return 'var(--kind-backend)'
  }
}

// Builds a service-name → kind lookup from the graph nodes so callers resolve a
// span/row's colour by key instead of scanning every node per render.
export function kindByName(nodes: GraphNode[] | undefined): Record<string, string> {
  const out: Record<string, string> = {}
  for (const n of nodes ?? []) out[n.name] = n.kind
  return out
}
