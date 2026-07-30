# Orbit v0.0.46 — make failure recovery one linear decision

Orbit now keeps the CLI and dashboard aligned on the first useful recovery
step. Users see the root cause, the evidence that can explain it, and one
command or control that moves the environment forward.

## What changed

- HTTP health failures remain distinct from process crashes across daemon
  status, CLI JSON, human status, and the dashboard. Orbit explains that the
  process is still running and will recover automatically when its health
  endpoint recovers.
- Status, logs, open, and dashboard controls follow the same recovery order:
  resolve a blocked root dependency, inspect available evidence, and retry
  only when no better diagnosis exists.
- Successful restart, environment apply, update, and rollback operations no
  longer invent a follow-up status command after the requested outcome is
  already complete.
- Misspelled resource names receive a high-confidence correction inline.
  `orbit logs appb`, for example, suggests and returns
  `orbit logs app-b` as the sole next action. Ambiguous names still fail closed
  and lead to the configured resource list.
- `orbit open` no longer treats infrastructure without an application URL as
  a broken configuration. It explains the distinction and leads to the
  dashboard, while stopped or degraded applications receive their relevant
  start or log action.
- Dashboard routes for optional workflows remain unavailable when the active
  environment does not configure them, including direct URL navigation.

## Why it matters

A developer should not need to distinguish daemon state from process state,
trace a dependency chain by hand, remember whether a resource is openable, or
translate a typo into a second discovery workflow. Orbit now makes those
decisions from the environment it already owns and presents one truthful next
step in both human and machine-facing surfaces.

## Evidence

- Clean first-five-minutes checkout, rejected checkout, state preservation,
  infrastructure-open fallback, and runtime recovery.
- Project switching followed by corrected up, down, restart, logs, and open
  commands for a misspelled resource.
- Live HTTP 500 health failure with a running process, truthful CLI evidence,
  dashboard recovery controls, and automatic return to healthy.
- Full journey suite and `make preflight`.

This is a pre-1.0 preview. It does not publish or authorize `v1.0.0`.
