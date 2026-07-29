# Orbit v0.0.29 — trust green after startup

Orbit now keeps checking resources after they first become healthy. The CLI
and dashboard no longer stay green when a running service or one of its
dependencies becomes unavailable.

## What changed

- HTTP, TCP, container exec, and Docker health checks continue for the life
  of the resource. Three consecutive failures degrade it by default; one
  successful probe recovers it.
- `health_check.failure_threshold` lets an environment tune the runtime
  failure budget. Log-pattern checks remain one-shot readiness signals.
- Running dependents show `degraded`, a clear reason, and `blocked_by` when a
  required dependency is unavailable.
- Dependency chains resolve to one root-cause recovery action, such as
  `orbit up shop-inventory-api --json`, instead of suggesting restarts for
  every affected service.
- Restoring the dependency returns still-running dependents to healthy
  automatically; Orbit does not restart processes that are already alive.
- CLI status, JSON contracts, the generic resources API, and the dashboard
  graph now share the same availability view.

## Faster contributor feedback

- The full local preflight keeps every existing check but runs independent
  work in parallel and reuses one Vitest process for Svelte transforms.
- On the reference development machine, the same preflight fell from 73.62
  seconds to 34.29 seconds.
- `make test` provides a focused Go + dashboard behavior loop; the complete
  `make preflight` remains the commit gate.

## Verification

- Unit and race tests cover failure debounce, recovery, dependency-chain
  propagation, root-cause actions, and generated wire contracts.
- The clean first-five-minutes acceptance test performs a real mini-shop
  checkout, stops inventory, verifies that order and the frontend are no
  longer green in CLI JSON and the dashboard graph, follows the targeted
  recovery, and confirms that the full environment returns to healthy.
