---
paths: ["**/*.go"]
---

# Go event-loop drop policy

**Rule**: Event loops with subscribers must document their drop policy in the type's comment.

**Why**: Non-blocking vs blocking send changes semantics. Without documentation, a subscriber that expects every event can silently miss them.

**Good**:
```go
// Subscribe returns a channel and unsubscribe func. The event loop uses
// non-blocking send; slow subscribers drop events. This is acceptable for
// observational subscribers (logging, history). Control-plane subscribers
// must not depend on receiving every event.
func (o *Orchestrator) Subscribe() (chan Event, func()) { ... }

func (o *Orchestrator) broadcast(evt Event) {
    for _, ch := range o.subs {
        select {
        case ch <- evt:
        default: // observational subscriber too slow; drop
        }
    }
}
```

**Bad**:
```go
// Subscribe returns an event channel.   ← what's the drop policy?
func (o *Orchestrator) Subscribe() (chan Event, func()) { ... }
```

**Control-plane subscribers**: use a separate blocking channel; never share an observational channel.

**See also**: `docs/CODE_CONVENTIONS.md` §12.
