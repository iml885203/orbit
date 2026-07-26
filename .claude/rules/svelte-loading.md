---
paths: ["**/*.svelte"]
---

# Svelte loading state

**Rule**: Use `$state` boolean flags (`loading`, `busy`) paired with `disabled` attribute and `aria-busy="true"`. Skip skeleton screens unless content height varies meaningfully.

**Why**: Loading state must be visible to mouse, keyboard, and screen-reader users. Skeletons are over-engineered for fixed-height UI.

**Good**:
```svelte
<script lang="ts">
    let busy = $state(false);
    async function onClick() {
        busy = true;
        try { await action(); } finally { busy = false; }
    }
</script>

<button disabled={busy} aria-busy={busy} onclick={onClick}>
    {busy ? 'Working…' : 'Start'}
</button>
```

**Bad**:
```svelte
<button onclick={onClick}>
    {loading ? <Spinner /> : 'Start'}
</button>
<!-- button still clickable while loading; no aria-busy -->
```

**See also**: `docs/CODE_CONVENTIONS.md` §16.
