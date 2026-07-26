---
paths: ["**/*.go"]
---

# Go error handling

**Rule**: Wrap errors with `%w`; never classify errors by string matching in production code.

**Why**: `%w` preserves the unwrap chain so callers can use `errors.Is` / `errors.As`. String matching couples callers to error message wording; one rename silently breaks classification.

**Good**:
```go
if err := client.Up(req); err != nil {
    return fmt.Errorf("up failed: %w", err)
}

var ErrDaemonUnreachable = errors.New("daemon unreachable")
if errors.Is(err, ErrDaemonUnreachable) { ... }
```

**Bad**:
```go
if err := client.Up(req); err != nil {
    return err  // no context
}

if strings.Contains(err.Error(), "no daemon running") {
    // brittle — see MR 66 review
}
```

**Exceptions**: Tests may match error strings; production code must not.

**See also**: `docs/CODE_CONVENTIONS.md` §9.
