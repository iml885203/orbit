# Development

[English](./development.md) · [繁體中文](./development.zh-TW.md)

Build Orbit from source, run tests, or maintain a custom distribution.

## Contributing to Orbit

### Testing unreleased main

Requires the Go version in `go.mod`, Node.js 22+, and pnpm 10+:

```bash
git clone https://github.com/iml885203/orbit.git
cd orbit
pnpm --dir ui install
make install
orbit version
orbit init
```

`make install` builds current source and deliberately makes it the one `orbit`
on your normal PATH. It preserves the replaced binary at
`~/.local/bin/orbit.prev` and prints a daemon restart reminder, so a published
release and a source build do not silently compete. Run `orbit version` after
switching to confirm which build you are testing.

For a one-off build that must not replace the installed release, use
`make build` and invoke `./bin/orbit` explicitly. Stop any daemon started by
that binary before returning to the installed `orbit`.

### Development workflow

```bash
make build         # Build frontend + Go binary
make test          # Go + dashboard behavior tests
make preflight     # Full source, static, installer, and documentation gate — once per coherent commit
make test-journeys # Installed binary through real Git, process, and Docker boundaries
make lint          # Run linter
make setup         # Install git hooks
```

Keep the edit loop proportional to the change: run the narrowest relevant
sociable test while editing, `make test` after a coherent product slice,
and `make preflight` once before committing it. Do not create a release for
each fix. Accumulate related fixes behind one user outcome, review the release
candidate as a whole, then publish one version.

Release numbering and compatibility promises are documented in
[Versioning and compatibility](versioning.md).

#### Dashboard development (hot reload)

```bash
cd ui && pnpm dev          # Vite dev server on :5173
orbit daemon start         # API backend + dashboard on :19800
```

## Build a custom Orbit distribution

When config alone is insufficient, a team can fork this repository and register its own feature set in `cmd/orbit`, passing additional `extension.Extension` values to `app.Main`. Each extension can contribute CLI commands, daemon setup and hooks, doctor and init behavior, and distribution defaults; see [internal/extension/extension.go](https://github.com/iml885203/orbit/blob/main/internal/extension/extension.go). The packages are internal, so this works only inside a fork: Orbit has no public Go API ([versioning](versioning.md#stable-contracts-from-v1)).

The UI build retains three compile-time seams in [ui/vite.config.ts](https://github.com/iml885203/orbit/blob/main/ui/vite.config.ts):

- `ORBIT_UI_EXT` replaces the `$ext` module with a team's navigation, routes, panels, settings sections, and lifecycle hooks.
- `ORBIT_UI_TYPES` replaces the generated-types barrel.
- `ORBIT_UI_OUTDIR` selects the dashboard build output embedded by the team's binary.

This route means maintaining a custom build and release line. Use it only for behavior that cannot be expressed in env configuration, and keep neutral improvements in this repo rather than duplicating the core.
