# Environment sources

[English](./environment-sources.md) · [繁體中文](./environment-sources.zh-TW.md)

An environment source distributes managed Orbit environments. Each source is
either a Git repository or a persistent local directory containing `envs/`.
It may bind one local application workspace shared by all its environments.
Managed environments use `<source>/<environment>` identities throughout CLI
JSON, daemon APIs, runtime state, and the dashboard. Different sources may
contain the same short name. A bare name resolves only from the default source.
Project `orbit.yaml` and explicit `-c <path>` configs remain independent.

## Add sources

```sh
orbit source add company \
  --url https://github.com/example/company-environments.git \
  --workspace /work/company \
  --default

orbit source add env-dev \
  --path /work/orbit-environments \
  --workspace /work/company
```

Without `--ref`, Git sync follows the repository default branch and reports
the resolved branch and commit. Local sync includes uncommitted files. Source
and workspace paths may be the same, but Orbit never infers one from the
other. Removing a local source never changes its user-owned directory.

## Manage and synchronize

```sh
orbit source list
orbit source info company
orbit source sync
orbit source sync env-dev
orbit source sync --all
orbit source update company --ref release/2026.08
orbit source update company --clear-ref
orbit source set-workspace company /worktrees/company-pr-42
orbit source clear-workspace company
orbit source set-default company
orbit source remove env-dev
```

Sync validates a staged cache before replacing the current cache. Failures
preserve the last valid environments and remain visible in source status.
Previous cache versions remain in Orbit-owned storage. `sync --all` attempts
every source and fails if any source fails. Only an update to the source that
owns the active managed environment can require `orbit env apply`.

## Select, inspect, and remove

```sh
orbit env list
orbit switch e2e
orbit switch env-dev/e2e
orbit env info env-dev/e2e
```

`env info` never selects, applies, or starts its argument. Observed endpoints
are included only when the daemon serves the same qualified identity. If sync
removes a selected stopped environment, its identity remains unavailable
until an explicit switch; Orbit never falls back to another source's short
name. A running environment retains its loaded identity and config until
switch or down even if a newer cache removes it.

A source owning a running environment cannot be removed. Removing the source
of a selected stopped environment requires confirmation or `--yes` and clears
the selection. A default source cannot be removed while another source exists;
set its replacement first. Orbit never guesses a replacement.

## Initialization and migration

```sh
orbit init --source company --url https://github.com/example/company-environments.git \
  --workspace /work/company --env e2e --yes
orbit init --source env-dev --path /work/orbit-environments \
  --workspace /work/company --env e2e --yes
```

Existing single-repository settings, cached environments, selection, and
workspace migrate once to `default` without network access. Legacy settings
are removed only after the source and selection are saved. `orbit env sync`
is now a non-executing migration guide: old flags only return the equivalent
`orbit source` command.
