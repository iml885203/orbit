# Orbit v0.0.36 — simpler defaults and a complete trial

Orbit removes a hidden tracing state and makes the public demo's path from
first run to real-project adoption coherent with the release being used.

## What changed

- Tracing now follows one rule: it is on unless an environment explicitly sets
  `enabled: false`. Adding a `tracing` section only to change its port or
  retention no longer turns the receiver off by accident.
- Contextual CLI help exposes `orbit trace` whenever tracing is active,
  including the zero-config default, while keeping the receiver-specific
  status command out of the beginner command surface.
- Healthy human status now gives one primary application action, matching
  startup and JSON output, instead of presenting every internal API URL as an
  equal next step.
- The README's five-minute journey now ends with `orbit down`, so a trial does
  not leave four host processes and Redis running.
- The official demo's English README, Traditional Chinese README, and in-app
  adoption action all link to the matching version of Orbit's ten-minute
  project guide.
- The demo beginner path no longer recommends the intentionally hidden
  agent-oriented `orbit inspect --json`; `orbit doctor` is the visible
  diagnosis path.
- The installed-user journey verifies that the synced demo and Orbit release
  share the same adoption contract, preventing future handoff drift.

## Why it matters

Users no longer need to remember a surprising third tracing state, infer how
to clean up a trial, or notice that a demo sent them to an older configuration
contract. The normal path stays within visible commands and carries one
versioned mental model from evaluation into real-project adoption.
