# Installation & development

[English](./development.md) · [繁體中文](./development.zh-TW.md)

Two audiences: **using Orbit** (install alternatives, upgrade / rollback / uninstall — the sections up to _Manual download_) and **hacking on Orbit itself** (build from source, dev workflow, dashboard hot reload — from _Build from source_ on). For the basic day-to-day workflow, see the [README](../README.md#your-first-5-minutes).

## Using Orbit

### Upgrading

Download and replace the binary with a newer release artifact, or, when the
binary's distribution config provides an install URL:

```bash
orbit update
```

After upgrading, the running daemon is still the old build. `orbit status` flags this with `⚠ newer orbit … — orbit daemon restart`. Run `orbit daemon restart` to pick up the new binary.

Each upgrade keeps the previous binary at `<path>.prev` (e.g. `~/.local/bin/orbit.prev`).

### Rollback

```bash
orbit update --rollback
orbit daemon restart
```

If orbit itself is broken, the equivalent manual steps are:

```bash
mv ~/.local/bin/orbit.prev ~/.local/bin/orbit
orbit daemon restart
```

### Uninstall

Run `orbit down` to stop services and containers, then remove the `orbit`
binary from your `PATH` and delete `~/.orbit/` if you also want to remove local
configuration and state. Docker images and git checkouts under your workspace
are not removed.

### Manual download

Download `orbit-<os>-<arch>` (or the Windows `.exe`) from the current project
Pages site and move the binary onto your `PATH`.

## Contributing to Orbit

### Build from source

Requires Go 1.25+ and Node.js 22+:

```bash
git clone https://github.com/iml885203/orbit.git
cd orbit
pnpm --dir ui install
make build        # compiles UI + binary into ./bin/orbit
./bin/orbit init
```

`make install` copies the dev build over `~/.local/bin/orbit` to use it as your daily orbit.

### Development workflow

```bash
make build      # Build frontend + Go binary
make test       # Run tests
make preflight  # Everything CI gates on (build, tests, vet, verify-types) — run before pushing
make lint       # Run linter
make setup      # Install git hooks
```

Release numbering and compatibility promises are documented in
[Versioning and compatibility](versioning.md).

#### Dashboard development (hot reload)

```bash
cd ui && pnpm dev          # Vite dev server on :5173
./bin/orbit daemon start   # API backend + dashboard on :19800
```
