# Orbit v0.0.44 — remove setup concepts from the happy path

Orbit now handles more local-machine differences on the user's behalf. The
default setup explains the value being prepared, dashboard startup coexists
with another Orbit instance, and movable project ports no longer require an
Orbit-specific environment-variable expression.

## What changed

- The default `orbit init` presents three user-facing steps — Quickstart,
  Environment, and Health check — while repository URL, revision, sync
  destination, and file count remain implementation details.
- If the normal dashboard port is occupied, Orbit selects another available
  port automatically. `orbit open`, status, inspect, and later CLI calls use
  the active address. An explicitly configured `ORBIT_DASHBOARD_PORT` remains
  pinned and fails closed on conflict.
- Project configuration can express a movable endpoint with readable
  `preferred` and `target` fields:

  ```yaml
  ports:
    http:
      preferred: 28080
  ```

  A simple number or `"host:target"` string remains fixed for tools that
  require a stable address.
- The local-first guide and runnable examples use the readable port form.
  Health checks, application URLs, dependency injection, and `PORT` all follow
  the selected runtime port without duplicate configuration.
- The local-first journey now remains deterministic when its preferred test
  ports were already occupied before the test began.

## Why it matters

A first-time user should think about their application and dependencies, not
the repository that delivered an environment, the port used by Orbit's own
dashboard, or the name of a special interpolation variable. Fixed ports remain
available when they are part of an external contract; movable ports state the
actual intent directly and let Orbit absorb ordinary workstation conflicts.

## Evidence

- Clean quickstart with another Orbit dashboard already bound.
- Project-local adoption with both preferred application and container ports
  occupied.
- Mini-shop success, rejected checkout, durable-state, and compensation smoke.
- Full journey suite and `make preflight`.

This is a pre-1.0 preview. It does not publish or authorize `v1.0.0`.
