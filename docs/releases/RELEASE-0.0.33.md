# Orbit v0.0.33 — reproducible demo environments

Orbit now keeps the environment definition used by a release stable and
visible. The official demo is fetched from the matching `v0.0.33` tag rather
than a moving default branch.

## What changed

- `orbit init` records the requested repository ref and the exact commit Git
  resolved.
- `orbit env list`, `orbit status`, and their JSON forms expose the managed
  repository URL, ref, and commit.
- `orbit env sync --ref <branch-or-tag-or-commit>` lets teams pin their own
  shared environment repository.
- Re-running sync for the same Orbit release resolves the same demo commit.
- Demo links now point to documentation for the matching Orbit release.
- Local `make preflight` keeps every existing gate while running independent
  checks concurrently; on the development baseline it dropped from about 61
  seconds to 38 seconds.
- Installed-user journeys now reuse one build and verify the command surface,
  environment provenance, linked demo behavior, dependency failure, recovery,
  local adoption, and project switching through real process and Docker
  boundaries.
- Docker polling uses the latest inspect result during startup, preventing a
  container that has already started from being misclassified by an older list
  snapshot.
- `orbit update` and `orbit update --rollback` now replace the binary actually
  invoked, reconnect a running environment, and restore its running resources
  automatically; the normal path no longer asks users to understand daemon
  lifecycle or run `orbit up` again.
- Daily `up`, `down`, `open`, and background-start messages no longer expose
  the internal control process. Repeating `down` after Orbit has already
  stopped is a successful human/JSON no-op that points only to the next normal
  `orbit up`.
- Environment schema mismatches now produce a structured, single-step remedy:
  sync an older shared environment or update an older Orbit binary. The
  previous nonexistent `orbit upgrade` instruction is gone.
- `orbit switch` no longer starts background state merely to record a
  selection. When Orbit is stopped it checks the selected environment and
  points only to `orbit up`, so choosing an environment cannot fail on an
  unrelated dashboard port.
- Doctor now reports only tools needed to run the selected environment. Git is
  explained at the shared-environment sync boundary instead of appearing as a
  daily runtime prerequisite; a missing Git installation receives a specific
  install-and-retry explanation.
- The dashboard now derives one primary environment action from live state:
  start a stopped environment or root dependency, inspect the degraded root,
  and open the application once healthy. Stop is secondary and
  infrastructure-only startup moves behind progressive disclosure.
- Tracing stays out of the primary navigation until it has data or needs
  attention. The application listens globally, reveals the workspace on the
  first span, and keeps direct links available for setup and diagnosis.
- The documented testing strategy favors end-to-end journeys and sociable
  domain tests over private-helper and DTO-copy tests, reducing maintenance
  without treating statement coverage as the product goal.
- Engine and health-check tests now synchronize on real events instead of
  fixed sleeps. Duplicate negative tests were removed and retry timing was
  shortened without weakening the lifecycle and cancellation contracts.
  Filesystem staleness uses explicit timestamps, and bounded database work
  uses a barrier that proves the worker cap without scheduler timing.
- The normal `down` completion contract is now owned by the installed-user
  journey. Redundant private-predicate tests were removed instead of
  preserving the implementation shape beneath that behavior.

## Why it matters

A working demo should not change because a repository branch moved after the
binary was released. Users and agents can now identify the exact environment
revision behind their local setup, reproduce it elsewhere, and distinguish a
deliberate sync from an invisible upstream change. The same user journey now
serves as a repeatable product-level release check instead of relying on a
large collection of implementation-coupled assertions.
