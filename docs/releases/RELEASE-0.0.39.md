# Orbit v0.0.39 — self-healing project context

Orbit now treats a project-local `orbit.yaml` as a first-class onboarding path
and keeps recovery actions valid when the user leaves that project directory.

## What changed

- Root and init help now explain that an existing project only needs an
  `orbit.yaml` at its root; `orbit init` is reserved for the bundled demo or a
  shared environment repository.
- `orbit doctor` from a completely clean home reports that setup is required
  and gives one `orbit init` action instead of exposing a missing internal
  quickstart path.
- After a project-local environment is stopped, `orbit status` outside the
  project reports the last project context and gives a copyable
  `cd <project> && orbit up` action.
- Detached project status no longer recommends restarting the daemon with a
  nonexistent managed-environment file.
- Recovery journeys with independent homes, namespaces, and ports now run in
  parallel, reducing that E2E group from about 40 seconds to about 22 seconds
  on the development machine.

## Why it matters

There are now two visible setup models instead of three accidental ones:
place `orbit.yaml` in a project, or initialize a shared/demo environment.
Doctor, status, and help agree on those models, and every recovery instruction
is executable from the user's current location.
