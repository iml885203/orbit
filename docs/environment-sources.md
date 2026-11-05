# Environment sources

[English](./environment-sources.md) · [繁體中文](./environment-sources.zh-TW.md)

Add an environment source to use and sync environments from a Git repository
or local directory containing `envs/`. Select a managed environment by
`<source>/<environment>`, or use its bare name to select it from the first
source. Project `orbit.yaml` and explicit `-c <path>` configs remain independent.

## Add sources

```sh
orbit source add company \
  --url https://github.com/example/company-environments.git

orbit source add env-dev \
  --path /work/orbit-environments
```

Without `--ref`, Git sync follows the repository default branch and reports
the resolved branch and commit. Local sync includes uncommitted files. Removing
a local source never changes its user-owned directory.

## Manage and synchronize

```sh
orbit source list
orbit source sync
orbit source sync env-dev
orbit source sync --all
orbit source remove env-dev
```

To change a source's location, remove it and add it again. Git and local
sources use the same workflow.

Sync validates a staged cache before replacing the current cache. Failures
preserve the last valid environments and remain visible in source status.
Previous cache versions remain in Orbit-owned storage. `sync --all` attempts
every source and fails if any source fails.

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
the selection. When the first source is removed, Orbit automatically uses the
next source for bare environment names.

## Initialization and migration

```sh
orbit init --source company --url https://github.com/example/company-environments.git \
  --workspace /work/company --env e2e --yes
orbit init --source env-dev --path /work/orbit-environments \
  --workspace /work/company --env e2e --yes
orbit init --source company --env e2e --yes
```

Naming a registered source without `--url` or `--path` re-syncs it as
registered; `--ref` (Git sources only) and `--workspace` replace the stored
values once the sync succeeds. Naming an unregistered source without a location
registers the default repository (the official demo unless overridden) under
that name, as a first bare `orbit init` does.

Existing single-repository settings, cached environments, selection, and
workspace migrate once to `default` without network access. Orbit reports what
it preserved and recommends inspecting and synchronizing the migrated source.
Legacy settings are removed only after the source and selection are saved.
