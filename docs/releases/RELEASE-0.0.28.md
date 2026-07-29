# Orbit v0.0.28 — move between projects without managing Orbit

Orbit now treats each project-local `orbit.yaml` as a safe working context.
Moving from one project to another no longer exposes a daemon/config mismatch
or asks users to restart Orbit with absolute config paths.

## What changed

- `orbit status` in a second project succeeds and shows only that project's
  resources. It tells you which other project is active and gives one next
  step: `orbit up`.
- `orbit doctor` validates the project you are standing in. Ports owned by the
  active Orbit project are recognized as ports Orbit will release during the
  switch, not unexplained conflicts.
- `orbit up` validates the new project first, then gracefully stops the
  previous project's resources and starts the current project.
- Runtime commands such as `down`, `logs`, `open`, `exec`, `query`, and
  `restart` cannot accidentally control another project's resources.
- JSON clients receive `context_switch_required`,
  `project_context_inactive`, and a successful `context_switch` summary with
  one safe recovery command.

## Why it matters

The user model is now the same in every checkout: enter the project and run
`orbit up`. Project ownership, daemon replacement, resource cleanup, and port
reuse are Orbit's responsibility.

## Verification

- A real two-project acceptance test starts project A, inspects project B,
  proves that a B-side `down` cannot stop A, switches with one `orbit up`, and
  verifies that the same port now serves project B.
- Unit coverage locks the human explanation, JSON action contract, port
  handoff, runtime-command guard, and lifecycle switch summary.
