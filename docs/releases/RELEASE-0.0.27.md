# Orbit v0.0.27 — the project selects itself

Orbit now treats an `orbit.yaml` beside a project as project context instead
of making users repeat a config flag on every command.

## What changes for users

The local development loop is now:

```text
orbit doctor
orbit up
orbit open app
orbit logs app
orbit down
```

Orbit finds the nearest `orbit.yaml` from the current directory, including
when the command runs from a nested project directory. There is no setup step,
environment switch, or repeated `-c ./orbit.yaml`.

## Predictable precedence

Config selection follows one explicit order:

1. `--config <path>` / `-c <path>`;
2. the nearest project `orbit.yaml`;
3. the managed environment selected with `orbit switch`;
4. the distribution default.

An explicit flag therefore remains the escape hatch. Project context does not
silently replace a config the user named.

## Visible active context

`orbit status --json` now identifies a discovered project config with:

```json
{
  "environment": {
    "state": "selected",
    "source": "project",
    "selected_path": "/absolute/path/to/project/orbit.yaml"
  }
}
```

This prevents a previously selected managed environment from being reported
as active while a project file is actually in use.

## One source of truth after promotion

The English and Traditional Chinese adoption guides now explain the boundary:
after the validated local file is safely committed to a team environment
repository, remove the project copy. The shared repository then becomes the
single source of truth rather than leaving two configs with unclear ownership.

## Verification

- Unit coverage for nearest-ancestor discovery, nearest-file precedence,
  missing project config, and active-context reporting.
- A real local-first acceptance journey launched from a nested directory with
  no `-c` flags.
- The same acceptance promotes the file to a managed team environment and
  proves the shared source after the project copy is removed.
- Occupied preferred ports, real host process, Redis container, HTTP request,
  logs, and shutdown remain part of the journey.
- Full `make preflight`, platform smoke, and release gates.

This remains a pre-1.0 preview and does not establish the `v1.0.0`
compatibility contract.
