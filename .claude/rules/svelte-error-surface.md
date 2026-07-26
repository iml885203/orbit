---
paths: ["**/*.svelte", "**/*.ts"]
---

# Svelte error surface

**Rule**: Fetch errors always surface to the user — via `toast()` for transient failures or a targeted panel error state for context-bound failures. Never silently swallow. Log to console in dev.

**Why**: Silent failures hide bugs and frustrate users who don't know why nothing happened.

**Good**:
```ts
async function loadStatus() {
    try {
        const res = await fetch('/api/status');
        return await res.json();
    } catch (e) {
        toast('service status unavailable');
        console.error('loadStatus:', e);
        return null;
    }
}
```

**Bad**:
```ts
async function loadStatus() {
    try {
        const res = await fetch('/api/status');
        return await res.json();
    } catch {
        return null;  // silent
    }
}
```

**See also**: `docs/CODE_CONVENTIONS.md` §13 (error UI also referenced).
