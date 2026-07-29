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
- The documented testing strategy favors end-to-end journeys and sociable
  domain tests over private-helper and DTO-copy tests, reducing maintenance
  without treating statement coverage as the product goal.

## Why it matters

A working demo should not change because a repository branch moved after the
binary was released. Users and agents can now identify the exact environment
revision behind their local setup, reproduce it elsewhere, and distinguish a
deliberate sync from an invisible upstream change. The same user journey now
serves as a repeatable product-level release check instead of relying on a
large collection of implementation-coupled assertions.
