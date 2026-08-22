# Use Orbit with your project

[English](./local-first.md) · [繁體中文](./local-first.zh-TW.md)

This ten-minute path starts with one file in your existing project. You do not
need an environment Git repository, `orbit init`, or settings under
`~/.orbit` for this local trial.

Install the [Orbit CLI](development.md#install-orbit) first. The example below
also requires Docker and Python 3; `orbit doctor` checks all three before it
starts the environment.

## 1. Put intent beside the project

Save this as `orbit.yaml` in your project root:

```yaml
version: "3"

containers:
  redis:
    image: redis:7.4-alpine
    ports:
      redis: "26379:6379"

services:
  app:
    kind: frontend
    command: python3 -m http.server "$PORT"
    ports:
      http: 28080
    depends_on: [redis]
```

This example serves the current project directory with Python and starts Redis
first. It uses tools already required by the public demo: Python 3 and Docker.
Orbit infers Python from the command and runs it from the directory containing
`orbit.yaml`; common Node, Bun, and Go commands work the same way. Add `type`
or `path` only when the command alone cannot express the intended runtime or
working directory.
Ports live in the file: Orbit injects the declared port as `PORT`, and a
conflict is an error naming the owning process — nothing moves silently.
Each endpoint is declared once: with one endpoint (or
an endpoint named `http`), Orbit waits for it before starting dependents and
reuses it for `orbit open` and dependency URLs. Add an explicit `health_check`
only when a listening port is not enough to prove application readiness.

## 2. Delegate the local loop

With the [Orbit skill](https://github.com/iml885203/orbit/blob/main/plugins/orbit/skills/orbit/SKILL.md)
installed or included in context, ask your coding agent:

> Use Orbit to inspect this project's prerequisites and current state. Follow
> only non-destructive recommended actions, start the environment, verify that
> `app` and `redis` are ready, and open `app`. Stop and explain before any
> destructive action or whenever setup requires my input.

This is the intended path: the agent reads structured state, performs the
narrowest safe action, and verifies the result. You remain responsible for
approving destructive operations and providing anything Orbit cannot infer.

To prove the same loop manually, run these commands from the project root:

```bash
orbit doctor
orbit up
orbit open app
orbit logs app
orbit down
```

Orbit finds the nearest `orbit.yaml` from the current directory, including
from a nested directory inside the project. An explicit `-c <path>` still wins
when you intentionally need another config. Orbit validates the exact service
directory and tools before startup, starts Redis before the host process, opens
the actual selected URL, and retains logs. It starts its local daemon
automatically.

You can move directly to another project that has its own `orbit.yaml`.
`orbit status` shows that the other project is still active without mixing its
resources into the current project, and `orbit doctor` checks whether the
current project is ready. Run `orbit up` to switch projects: Orbit validates
the new project first, stops the previous resources, and starts the current
ones. When the previous project has running resources, Orbit names both
projects and asks before stopping them; scripts use `orbit up --yes`.
Commands such as `down`, `logs`, and `open` never control resources that
belong to the other project.

At this point the whole mental model is:

```text
your-project/
├── your code
└── orbit.yaml          environment intent for this local trial
        │
        ├── app         host process beside this file
        └── redis       Docker container

~/.orbit/               Orbit-managed runtime state; do not edit it
```

Replace the example `command`, health check, and dependency with your real
project only after this loop works. See [Configuration](configuration.md) for
the available fields. While the environment is running, edit `orbit.yaml` and
run `orbit up` again: Orbit validates the new file before stopping anything,
then applies it. Unchanged resources keep running; changed resources and their
dependents restart only when needed. Daemon-level changes such as tracing or
the Docker poll interval use a full handoff and restore the running resources.

## 3. Share the validated environment

Keep experimenting with the project-local file for as long as it is useful.
When teammates should receive the same environment, put the validated config
in a separate Git repository:

```text
your-orbit-env/
└── envs/
    └── dev.yaml
```

After copying it, add this service path:

```yaml
path: ${WORKSPACE_ROOT}
```

Relative paths follow the config file, so `.` is correct while `orbit.yaml`
lives in the project—and is the default when `path` is omitted. A synced config
lives under Orbit's managed environment directory; `${WORKSPACE_ROOT}`
explicitly points it back to each developer's checkout.

Commit and push `envs/dev.yaml`. Remove the project-local `orbit.yaml` after
the shared copy is safely committed so the team repository becomes the single
source of truth. Then initialize from the project checkout:

```bash
orbit init --source team --url https://github.com/you/your-orbit-env.git --env dev
orbit up
```

Orbit asks for the workspace only because `dev.yaml` proves that it needs
`${WORKSPACE_ROOT}`. Enter the absolute path of this project checkout; when
you run init inside a Git checkout, pressing Enter accepts that checkout as the
default. Each developer answers once; the shared file contains no
machine-specific absolute path. During environment-repository development,
`orbit source add local --path /path/to/your-orbit-env` tests local, even
uncommitted files.

The two stages have different jobs:

```text
project-local orbit.yaml     learn and validate without repository ceremony
shared envs/dev.yaml         distribute stable team intent through Git
```

For authentication, multi-environment layout, and advanced customization, see
[Team adoption](team-adoption.md).
