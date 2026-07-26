---
paths: ["**/*.go"]
---

# Go callbacks vs interfaces

**Rule**: For intra-package signalling, prefer function-field callbacks (`OnOutput`, `OnExit`, `OnAction`) over thin interfaces. Define interfaces only for test mocking or cross-package boundaries.

**Why**: Callbacks are simpler, have less indirection, and align with §6 composition. A thin interface that exists only to "name the layer" costs more than it provides.

**Good**:
```go
// process/manager.go
type Manager struct {
    OnOutput func(name string, line string)
    OnExit   func(name string, err error)
}

func NewManager() *Manager { return &Manager{} }

// Caller wires up callbacks directly:
mgr := process.NewManager()
mgr.OnOutput = func(name, line string) { logger.Print(line) }
```

**Bad**:
```go
type ProcessObserver interface {
    OnOutput(name string, line string)
    OnExit(name string, err error)
}
type Manager struct {
    observer ProcessObserver
}
```

**Define an interface when**:
- You need to swap implementations in tests (e.g. `health.ContainerInspector` for mocking Docker).
- The boundary crosses a package and the caller and implementation live in different packages.

**See also**: `docs/CODE_CONVENTIONS.md` §11.
