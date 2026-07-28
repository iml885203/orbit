# Installation & development

[English](./development.md) · [繁體中文](./development.zh-TW.md)

Two audiences: **using Orbit** (install alternatives, upgrade / rollback /
uninstall) and **hacking on Orbit itself** (build from source, dev workflow,
dashboard hot reload). For the basic day-to-day workflow, see the
[README](../README.md#common-workflows).

## Using Orbit

### Platform support

| Platform | Support | Installation |
|---|---|---|
| macOS arm64 / amd64 | Supported | `install.sh` or manual release download |
| Linux arm64 / amd64 | Supported | `install.sh` or manual release download |
| Windows amd64 / arm64 | Beta | Native `install.ps1` or manual `.exe` download |

Container-based environments require Docker Desktop on macOS and Windows, or
Docker Engine on Linux. Every environment may declare additional host runtimes;
`orbit doctor` names them and provides installation guidance. For Node services
started through npm, pnpm, Yarn, or Bun, it also distinguishes a missing runtime
from project packages that have not been installed and reports the exact install
command. `orbit switch` reports these checks for the newly selected environment
before the user runs `orbit up`. Runtime checks honor `.nvmrc`, `.node-version`,
`.python-version`, `.bun-version`, relevant entries in `.tool-versions`, and
.NET `global.json`. Orbit reports mismatches and conflicting declarations but
does not install or switch runtimes. Git is required to sync environment
repositories.

Windows builds receive release smoke coverage, but do not yet promise full
macOS/Linux runtime parity. The native PowerShell installer verifies the
release checksum and version, preserves the previous binary, refuses accidental
downgrades, and adds Orbit to the user PATH. Windows container workloads use
Docker Desktop.

### Upgrading

Download and replace the binary with a newer release artifact, or, when the
binary's distribution config provides an install URL:

```bash
orbit update
```

On Windows Beta, rerun `install.ps1` to update; replacing a running `.exe`
in-place is not reliable on Windows, so `orbit update` is not yet supported
there.

After upgrading, the running daemon is still the old build. `orbit status` flags this with `⚠ newer orbit … — orbit daemon restart`. Run `orbit daemon restart` to pick up the new binary.

Each upgrade keeps the previous binary at `<path>.prev` (e.g. `~/.local/bin/orbit.prev`).
The installer verifies the checksum and the downloaded binary's reported
version before touching the current install. Replacement is staged beside the
target for an atomic rename. It refuses to replace a newer installed version
unless the downgrade is explicit.

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

To return to a specific release rather than the immediately previous one:

```bash
curl -fsSL https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.sh |
  ORBIT_VERSION=v0.9.0 ORBIT_ALLOW_DOWNGRADE=1 bash
orbit daemon restart
```

The named artifact still has to pass its release checksum and version check;
the replaced binary becomes the new `.prev`.

### Uninstall

Preview the exact paths first, then remove the binary while preserving
environments, settings, and local state:

```bash
orbit uninstall
orbit uninstall --yes
```

Add `--purge` only when you also intend to permanently remove `~/.orbit/`:

```bash
orbit uninstall --yes --purge
```

The command stops Orbit first. Windows Beta schedules removal of the running
`.exe` immediately after the command exits. Docker images and git checkouts
under your workspace are never removed.

### Manual download

Download `orbit-<os>-<arch>` (or the Windows `.exe`) and `checksums.txt` from
the matching [GitHub Release](https://github.com/iml885203/orbit/releases).
Verify the SHA-256 checksum before moving the binary onto your `PATH`.

### Agent plugin

Each source release includes `plugins/orbit-agent`, with manifests for both
Codex and Claude Code. Add that directory as a local plugin using your agent's
plugin command. The two manifests and Orbit release always carry the same
version; do not mix a plugin from one release with an older binary.

## Contributing to Orbit

### Build from source

Requires Go 1.25+, Node.js 22+, and pnpm 10+:

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
