package extension

import "context"

// DaemonHooks is what a feature set registers with the daemon. B3a wires
// the event-source and shutdown seams; routes, doctor checks, and
// resource contributors follow with DaemonSetup (B3b).
type DaemonHooks struct {
	// EventSources are multiplexed onto the daemon's /api/events SSE
	// stream. Each source's Run is invoked once per SSE connection.
	EventSources []EventSource

	// OnDown runs FIRST in the daemon-exit path — before service and
	// process teardown begins, not merely before ctx cancel. Tunnel
	// release depends on this: its mgmt port-forwards live in the shared
	// process manager and die in teardown, so releasing after teardown
	// would degrade to the remote TTL fallback. The raw-signal path
	// (SIGKILL etc.) still skips these hooks.
	OnDown []func()
}

// Merge folds another feature set's hooks into these, preserving order:
// other's event sources and shutdown hooks run after the receiver's. The
// daemon uses it to accumulate every extension's hooks; a composition root
// uses it to combine sibling features into one DaemonHooks. Any field
// DaemonHooks grows must be appended here so no caller silently drops it.
func (h *DaemonHooks) Merge(other DaemonHooks) {
	h.EventSources = append(h.EventSources, other.EventSources...)
	h.OnDown = append(h.OnDown, other.OnDown...)
}

// EventSource feeds one named SSE channel. The forward loop is the
// contract — not a channel — so the upstream's semantics (a snapshot
// store's buffer-1 coalescing, an operation feed's bounded buffer, an
// access log's drop-on-full) survive inside the source. Run must block until ctx is done; emit
// blocks on a slow client so back-pressure reaches the upstream
// subscription, and returns false once the connection is gone — the
// loop must stop then, not take another value off the subscription.
// Run is launched on its own goroutine per connection, so the subscribe
// happens asynchronously relative to the connection opening — sources
// must replay current state on subscribe rather than assume no frame
// precedes their subscription.
type EventSource struct {
	Name string // SSE event type the client dispatches on
	Run  func(ctx context.Context, emit func(data any) bool)
}

// RunChannel adapts the typed Subscribe pattern (`Subscribe() (<-chan T,
// func())`) to an EventSource Run. Each invocation takes a fresh
// subscription, so per-connection coalescing windows stay per-connection.
func RunChannel[T any](subscribe func() (<-chan T, func())) func(ctx context.Context, emit func(data any) bool) {
	return func(ctx context.Context, emit func(data any) bool) {
		ch, cancel := subscribe()
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case v, ok := <-ch:
				if !ok {
					return
				}
				if !emit(v) {
					return
				}
			}
		}
	}
}
