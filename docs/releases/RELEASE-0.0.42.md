# Orbit v0.0.42 — make recovery match reality

Orbit now gives one truthful recovery path for setup, project context, runtime
health, and optional features instead of asking users to infer what kind of
failure they are looking at.

## What changed

- A fresh `orbit status` leads only to `orbit init`; it no longer mixes setup,
  sync, and startup concepts before an environment exists.
- Project-local schema-2 files point directly to the schema-3 migration guide.
  Managed environments still use `orbit env sync`, and JSON clients receive a
  stable non-retryable migration error instead of a command loop.
- A single service port now always provides `PORT`, including literal host
  ports, while explicit environment overrides remain authoritative.
- Orbit remembers the running project context when commands run outside that
  checkout. Status and Doctor show the exact directory or `--config` command
  needed to operate it; an explicit conflicting config still fails closed.
- Runtime failures now carry their source. A process crash still leads to exit
  logs, while a live process returning HTTP 500 shows the probe URL, explains
  that Orbit keeps checking, and treats restart as a retry rather than a fix.
  The dashboard opens health details instead of misclassifying the service as
  crashed.
- Tunnel commands and APIs now fail closed when the active environment has no
  `claim` configuration, matching the command visibility and dashboard gate.
- A mistyped `orbit switch` lists the available environment names and gives an
  exact `orbit env list` action without changing the current selection.
- Environment updates distinguish resources that were restored from newly
  required dependencies that Orbit started because the configuration changed.
- Releases require successful CI evidence from the version-matched demo tag,
  preventing a moving demo branch from silently validating a different pair.

## Why it matters

Users should not need to know Orbit's daemon internals, health state machine,
environment storage, or extension wiring to recover. The command output and
dashboard now describe the state that is actually true: whether setup is
missing, a file needs migration, a previous project is still active, a process
exited, a health endpoint is failing, or an optional workflow is unavailable.
That keeps each next action honest and makes environment changes explainable
without requiring users to reconstruct the dependency graph themselves.
