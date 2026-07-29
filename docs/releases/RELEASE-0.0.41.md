# Orbit v0.0.41 — remove setup guesswork

Orbit now detects more host-project requirements before startup, keeps recovery
actions executable, and removes database-specific seed configuration from the
core environment schema.

## What changed

- Go services are checked as host runtimes. A `go.mod` language or toolchain
  requirement is compared with the installed Go version before `orbit up`.
- Direct Node commands such as `node server.js` now receive the same package
  readiness as package-manager commands. Orbit reads the nearest workspace
  `packageManager` or lockfile and supports root-level installed packages.
- When the selected package manager is missing, Orbit leads with that
  prerequisite instead of recommending a package-install command that cannot
  run. Once available, Doctor provides the exact npm, pnpm, Yarn, or Bun setup
  command.
- Node and Python dependency failures share the same recovery path: install the
  declared packages, then restart only the affected service.
- Environment schema 3 replaces SQL Server/Mongo-specific seed fields with one
  database-neutral in-container `command`; seed files are provided on standard
  input. Command changes invalidate recorded seed state without silently
  applying data.
- Project-local schema-2 files now point to the documented manual migration
  instead of the ineffective `orbit env sync` action. Managed shared
  environments still advance with one sync.
- Contributor feedback is faster: isolated process and runtime journeys run in
  parallel, the dashboard suite uses shared VM threads, and superseded CI runs
  are cancelled. On the development baseline, a cold Go suite fell from about
  22 seconds to 11 seconds and the UI suite from about 13 seconds to 9 seconds.

## Why it matters

A developer switching into a Go, Node, Python, or .NET environment should learn
what is missing before any resource starts, and every suggested command should
be runnable at that moment. Maintainers can seed any container image with the
client it already ships instead of teaching Orbit a database type or storing
database credentials in core seed fields. The schema-3 break is explicit and
documented while Orbit is still a preview, and the shorter feedback loop lets
the remaining 1.0 UX work land in meaningful batches.
