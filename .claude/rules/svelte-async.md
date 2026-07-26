---
paths: ["**/*.svelte", "**/*.ts"]
---

# Svelte SSE vs poll

**Rule**: SSE for real-time multi-consumer state; polling for long-duration tasks. Don't mix patterns on the same data source. Retry transient failures with exponential backoff (3× max). Surface unrecoverable errors via toast or panel error state — never silently swallow.

**Why**: Mixed patterns make the data-flow story unreadable. Silent swallowing hides bugs; aggressive infinite retry burns the daemon.

**Good** (SSE):
```ts
const stopSSE = createSSE(
    '/api/status',
    (status) => replaceServices(status.services),
    () => console.log('connected'),
    () => toast('lost daemon connection'),
);
```

**Good** (polling):
```ts
async function pollOperation(signal: AbortSignal) {
    while (!signal.aborted) {
        const res = await fetch('/api/operations/status');
        const status = await res.json();
        if (status.done) return status;
        await sleep(2000);
    }
}
```

**Bad**:
```ts
try {
    const r = await fetch('/api/status');
    // ...
} catch { /* ignore */ }  // silent swallow
```

**See also**: `docs/CODE_CONVENTIONS.md` §13.
