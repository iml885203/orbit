# Orbit v0.0.38 — resilient local runtime recovery

Orbit now keeps its runtime status truthful through stale daemon metadata,
system sleep, and Docker interruptions, then recovers automatically when the
underlying runtime returns.

## What changed

- `orbit up` safely replaces stale daemon metadata only after proving the
  recorded process owns Orbit's dashboard port. A reused PID belonging to
  another application is left untouched.
- Docker polling tolerates one transient transport failure. After a confirmed
  outage, previously healthy containers are shown as degraded with Docker as
  the root cause instead of retaining a stale healthy state.
- `orbit status` leads a Docker outage to one `orbit doctor` diagnostic rather
  than recommending logs or restarts for every affected container.
- Orbit keeps polling and automatically restores container state when Docker
  returns. A partial first snapshot during Docker startup is not mistaken for
  the user removing every container.
- Container health-check failures cannot overwrite a known Docker outage with
  lower-level connection noise.
- External container restarts update uptime and restart evidence without
  requiring an Orbit restart.
- Release journeys now exercise external Docker restarts and a host service
  that dies while the daemon is suspended, modeling system sleep and resume.

## Why it matters

Runtime recovery has one mental model: fix the underlying runtime and let
Orbit converge. Users do not need to decide whether stale state requires
deleting files, restarting the daemon, or restarting every resource. Status
stays honest, diagnostics point at the root cause, and unrelated local
processes remain safe.
