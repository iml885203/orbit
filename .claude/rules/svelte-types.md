---
paths: ["**/*.svelte", "**/*.ts"]
---

# Svelte component-scoped types

**Rule**: Define type aliases inside the `.svelte` script unless used by 3+ consumers; then extract to `lib/<domain>-types.ts` with a domain prefix. Exported component types must comment their consumers.

**Why**: Premature extraction creates dead types in `types.ts`. Co-location keeps the type next to its only user.

**Good**:
```svelte
<script lang="ts">
    type Cta = { label: string; onClick: () => void };
    let cta = $derived<Cta>({ ... });
</script>
```

```ts
// lib/graph-types.ts — extracted because GraphNode is used by GraphView,
// NodeDrawer, and graphActions.ts.
export type GraphNode = { id: string; state: ServiceState };
```

```ts
// Used by GraphView.svelte for node-anchor coordination.
// Comment exported types with their consumers.
export type ProjectPipeAnchor = { ... };
```

**Bad**:
```ts
// lib/types.ts — magnet file with 200 unrelated exports
export type Cta = ...;
export type Foo = ...;
export type Bar = ...;
```

**See also**: `docs/CODE_CONVENTIONS.md` §14.
