---
paths: ["**/*.go"]
---

# Go domain model

**Rule**: When a non-trivial multi-step operation repeats across 3+ call sites,
expose it as an entry point on the owning domain — not as a CLI-side helper
or inlined boilerplate. The domain owns its own error vocabulary (sentinel
errors, hints).

**Why**: Boilerplate that repeats in three or more places is a signal the
domain hasn't exposed the right method. CLI-side helpers like `requireFoo()`
become magnets for similar repetitions and end up as `helpers.go` /
`utils.go`. Pushing the operation down also lets the domain own its sentinel
errors and the messages users see.

**Good**:
```go
// daemon/client.go
var ErrDaemonUnreachable = errors.New("daemon unreachable")

func Dial(socketPath string) (*Client, error) {
    c := NewClient(socketPath)
    if err := c.Health(); err != nil {
        return nil, fmt.Errorf("%w — start with 'orbit up' or 'orbit daemon start': %w",
            ErrDaemonUnreachable, err)
    }
    return c, nil
}

// caller
client, err := daemon.Dial(daemon.DefaultSocketPath())
if err != nil { return err }
```

**Bad**:
```go
// six different files
client := daemon.NewClient(daemon.DefaultSocketPath())
if err := client.Health(); err != nil {
    return fmt.Errorf("no daemon running — start with 'orbit up'")
}

// plus a CLI-side helper that does the same thing in a 7th file
func requireDaemonRunning() (*daemon.Client, error) { ... }
```

**When NOT to add an entry point**:
- One production caller. A function with a single non-test caller belongs in that caller's file.
- Trivial wrappers (1–3 lines around stdlib). Inline them — `c := exec.Command(...); c.Stdout, c.Stderr = os.Stdout, os.Stderr; return c.Run()` is clearer at the call site than `runPiped(...)`.
- Operations that vary meaningfully between callers (different timeouts, retry policies, output sinks). Pass options or split into distinct entry points; don't merge under one signature with branches.

**See also**: `docs/CODE_CONVENTIONS.md` §17, §6.
