# Orbit v0.0.32 — know the prerequisite before startup

Orbit now makes the public demo's requirements visible before installation and
turns a missing environment runtime into one direct setup path.

## What changed

- The English and Traditional Chinese quickstarts name Git, Docker, and Python
  3 before the install command, with official installation links.
- These requirements describe the public demo, not Orbit globally. Custom
  environments continue to drive their own runtime checks.
- When exactly one external prerequisite is missing, `orbit init` repeats its
  concrete installation hint instead of collapsing it into a generic
  `orbit doctor` step.
- Setup makes the continuation explicit: install the missing prerequisite,
  then run `orbit up`. The selected environment is already saved, so users do
  not repeat initialization.
- Multiple missing prerequisites remain a diagnostic list; Orbit does not
  pretend one hint resolves all of them.
- JSON setup failures always expose an executable recovery action. A missing
  external runtime recommends `orbit doctor --json` after installation instead
  of emitting an empty action.

## Verification

- A release-only v0.0.31 run with Git and Docker available but Python removed
  from `PATH` reproduced the generic completion and extra Doctor step.
- The updated binary derived Python from four services in the synced demo
  environment and printed the official Python installation link, followed by
  `Then: orbit up`.
- Adding Python back to `PATH` and running `orbit up` directly—without
  repeating init—made Redis and all four host services healthy and recommended
  opening `demo-shop`.
- The JSON journey reports the failed Python check and a non-empty
  `orbit doctor --json` recovery action.
- The installed-user acceptance suite now masks Python from `PATH` and protects
  the single-prerequisite path before running the complete checkout journey.
