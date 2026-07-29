# Orbit v0.0.31 — one recovery path back to the app

Orbit now distinguishes a stopped dependency from one that is still running
but unhealthy, then guides the user through the correct recovery and back to
their primary frontend.

## What changed

- A dependency-blocked Dashboard drawer offers `Restart <dependency>` when the
  root process is alive but unhealthy. Stopped roots still offer `Start`.
- The action targets the root cause, not the dependent that happens to be
  selected.
- Restarting the root restores still-running dependents without restarting
  them.
- Status resources now expose a stable `role` (`frontend`, `backend`, or
  `infra`) alongside the runtime `kind` (`service` or `container`).
- After a targeted backend recovery, Orbit returns to the environment's only
  healthy frontend. In environments with multiple unrelated frontends, Orbit
  keeps the requested resource instead of guessing.

## Public demo

- An unavailable checkout now points to `orbit status` as the single source of
  recovery guidance.
- The page no longer assumes the order API failed when the actual blocker may
  be catalog, inventory, Redis, or another dependency.
- After recovery, Orbit recommends reopening `demo-shop`, not an internal API.

The demo delivery is available in
[`iml885203/orbit-demo@acabf73`](https://github.com/iml885203/orbit-demo/commit/acabf73).

## Verification

- A real process was paused while retaining its port, producing an HTTP health
  timeout and a blocked dependent.
- The dependent drawer offered `Restart health-root`; clicking it replaced the
  paused process, restored both resources to healthy, and left the dependent's
  PID unchanged.
- A real mini-shop environment stopped and restarted only inventory. The JSON
  recovery result recommended `orbit open demo-shop --json`, and status
  reported the frontend/backend/infra roles.
- Unit coverage protects stopped versus live-unhealthy actions, single
  frontend selection, multi-frontend ambiguity, status roles, and demo failure
  guidance.
