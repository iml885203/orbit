# Adopting orbit for your team

[English](./team-adoption.md) · [繁體中文](./team-adoption.zh-TW.md)

Orbit is a local dev orchestrator: one YAML env file describes your containers and services, and `orbit up` starts them in dependency order with health checks, logs, tracing, and a dashboard at <http://localhost:19800>.

This single repo contains the neutral engine, CLI, daemon, and UI together with the ExampleTeam feature set behind explicit extension seams. Other teams normally consume the released Orbit binary and keep only their environment configuration in a separate env repo.

## Config-only adoption

1. Create a git repo with an `envs/` directory of env YAML files. [examples/quickstart/dev.yaml](examples/quickstart/dev.yaml) is a minimal starting point.
2. Point Orbit at it and select an environment:

   ```sh
   orbit env sync --url <your-env-repo-git-url>
   orbit switch dev
   orbit up
   ```

   `orbit init` walks through the same setup interactively. During development, `orbit env sync --path /path/to/your-env-repo` can use a local checkout. If the active environment changed, sync offers to make it current and restores only the resources that were running. Use `--no-apply` only when an interruption must be deferred; Orbit prints the exact command to finish later.

The released binary supplies its distribution defaults. A custom build without those defaults can set `env_repo_url` or `ORBIT_ENV_REPO_URL` for `orbit env sync`, and `ORBIT_INSTALL_URL` for `orbit update`. Services, containers, graph, logs, health checks, doctor, and the dashboard otherwise need only the env configuration. Tracing is on by default; an env opts out with an explicit `tracing.enabled: false`.

## Compile-time customization

The extension contract remains available when config alone is insufficient. A team can fork this repo or require it as a Go module, provide its own `cmd/orbit`, and pass additional `extension.Extension` values to `app.Main`. Each extension can contribute CLI commands, daemon setup and hooks, doctor and init behavior, and distribution defaults; see [extension/extension.go](../extension/extension.go).

The UI build retains three compile-time seams in [ui/vite.config.ts](../ui/vite.config.ts):

- `ORBIT_UI_EXT` replaces the `$ext` module with a team's navigation, routes, panels, settings sections, and lifecycle hooks.
- `ORBIT_UI_TYPES` replaces the generated-types barrel.
- `ORBIT_UI_OUTDIR` selects the dashboard build output embedded by the team's binary.

This route means maintaining a custom build and release line. Use it only for behavior that cannot be expressed in env configuration, and keep neutral improvements in this repo rather than duplicating the core.
