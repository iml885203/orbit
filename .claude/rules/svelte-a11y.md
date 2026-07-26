---
paths: ["**/*.svelte"]
---

# Svelte accessibility ground rules

**Rule**:
- All interactive elements have visible text or `aria-label`.
- Modals use `role="dialog"` + `aria-modal="true"` and either `aria-label` or `aria-labelledby` pointing at the heading.
- Status indicators use `role="status"`.
- Form inputs have an associated `<label>` (visible or `aria-labelledby`).

**Why**: Screen readers and keyboard users depend on these. Silent UI is broken UI.

**Good**:
```svelte
<button aria-label="Close drawer" onclick={onClose}>×</button>

<div role="dialog" aria-modal="true" aria-labelledby="title-id">
    <h2 id="title-id">Node details</h2>
</div>

<span role="status">{state}</span>
```

**Bad**:
```svelte
<button onclick={onClose}>×</button>  <!-- no label -->
<div class="modal">...</div>          <!-- no role -->
```

**See also**: `docs/CODE_CONVENTIONS.md` §15.
