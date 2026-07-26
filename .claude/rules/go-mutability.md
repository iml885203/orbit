---
paths: ["**/*.go"]
---

# Go mutable state

**Rule**: Mutable shared state must be encapsulated in a receiver type that owns the lock. Document lock preconditions in comments on any exported field requiring a held lock.

**Why**: Go has no `@GuardedBy` annotation. Comments are the only signal that an exported field needs a held lock. Undocumented locks lead to races.

**Good**:
```go
// internal/engine/orchestrator.go
type Orchestrator struct {
    mu       sync.RWMutex // guards services
    services map[string]*ServiceInfo
}

// ServiceInfo fields are public for read; mutation requires holding the
// owning Orchestrator.mu. See Orchestrator.UpdateDetachedDeps.
type ServiceInfo struct {
    Name  string
    State string
}
```

**Bad**:
```go
// Globally mutable, no protection
var serviceStates = map[string]string{}

// Public mutable field, no lock documentation
type ServiceInfo struct {
    State string  // who can mutate this? Under what lock?
}
```

**Exceptions**: Singleton types like `daemon.Settings` are explicitly global by design — they own their own locks.

**See also**: `docs/CODE_CONVENTIONS.md` §10.
