# Installation & development

[English](./development.md) · [繁體中文](./development.zh-TW.md)

Two audiences: **using Orbit** (install alternatives, upgrade / rollback /
uninstall) and **hacking on Orbit itself** (build from source, dev workflow,
dashboard hot reload). For the basic day-to-day workflow, see the
[website overview](https://orbit.dotw.me/#get-started).

## Using Orbit

The documented installers always install a published GitHub Release. Their
scripts live on `main`, but they do not install an unreleased `main` build. To
test current source, follow [Testing unreleased main](#testing-unreleased-main).

### Install Orbit

macOS or Linux with Homebrew:

```bash
brew install iml885203/tap/orbit
```

Windows with Scoop (Beta):

```powershell
scoop bucket add iml885203 https://github.com/iml885203/scoop-bucket
scoop install orbit
```

macOS or Linux with the verified installer:

```bash
curl -fsSL https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.sh | bash
export PATH="$HOME/.local/bin:$PATH"
```

Windows PowerShell (Beta):

```powershell
irm https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.ps1 | iex
```

### Platform support

| Platform | Support | Installation |
|---|---|---|
| macOS arm64 / amd64 | Supported | Homebrew, `install.sh`, or manual release download |
| Linux arm64 / amd64 | Supported | Homebrew, `install.sh`, or manual release download |
| Windows amd64 / arm64 | Beta | Scoop, native `install.ps1`, or manual `.exe` download |

Container-based environments require Docker Desktop on macOS and Windows, or
Docker Engine on Linux. Every environment may declare additional host runtimes;
`orbit doctor` names them and provides installation guidance, including Go.
For Node services, it distinguishes a missing runtime from project packages
that have not been installed and reports the exact npm, pnpm, Yarn, or Bun
install command. This also works when the service command starts with `node`;
Orbit reads `packageManager` or the nearest workspace lockfile instead of
requiring the package manager to appear in the command, and verifies that
manager is installed. `orbit switch` reports these checks for the newly
selected environment before the user runs `orbit up`. Runtime checks honor
Go's `go.mod`, `.nvmrc`, `.node-version`,
`.python-version`, `.bun-version`, relevant entries in `.tool-versions`, and
.NET `global.json`. Orbit reports mismatches and conflicting declarations but
does not install or switch runtimes. These reports are warnings and do not
block `orbit up`; only a missing required runtime does. Git is required to sync
environment repositories.

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

Package-manager installations remain owned by their package manager. For
those installations, `orbit update` leaves the binary unchanged and reports
the exact command to run: `brew upgrade orbit` or `scoop update orbit`.
`orbit update --rollback` likewise refuses to modify a package-managed binary.

`orbit update` replaces the binary that is actually running, even when another
Orbit installation appears earlier on `PATH`. If an environment is running,
the command reconnects it with the new binary and restores exactly the
resources that were running. A normal update therefore needs no separate
daemon command or second `orbit up`.

On Windows Beta, Orbit copies a detached updater outside the installation,
waits for the invoking process and registered daemons to exit, then replaces
the `.exe`, verifies it, and restores the prior runtime intent. This avoids
overwriting an executable while Windows still has it open.

After a manual binary replacement, `orbit status` may report
`Orbit update ready`; resource mutations pause rather than crossing versions.
Follow the exact recovery command it prints. `orbit doctor` also identifies a
different installation shadowing the binary you invoked.

Each upgrade keeps the previous binary at `<path>.prev` (e.g. `~/.local/bin/orbit.prev`).
The installer verifies the checksum and the downloaded binary's reported
version before touching the current install. Replacement is staged beside the
target for an atomic rename. It refuses to replace a newer installed version
unless the downgrade is explicit. When the installed and released versions
match, the installer exits successfully without replacing the binary or
changing its `.prev` backup.

#### Automatic updates

Official release builds check for a newer release at most once every 24 hours.
The foreground command performs no release network request: one detached check
downloads the matching artifact and checksum, verifies both the checksum and
the candidate binary's reported version, and cryptographically verifies the
immutable-release attestation that binds the tag, commit, artifact, and checksum
file. It stages the bytes and bounded evidence in the user-global Orbit update
registry. Source, dirty, and unbranded builds have no implicit release channel.

Automatic updates default to on. Orbit applies a verified staged update only
after a command finishes, every registered product environment has zero running
or restoring resources, and daemon convergence is idle. Idle daemons are moved
to the target build; a running product environment defers apply and exposes one
`orbit update` / **Update now** action that restores its prior running intent.
Read-only `status --json` and `inspect --json` continue reporting the durable
transaction while mutations wait for replacement or rollback to finish.
Delayed apply revalidates the local staged bytes without a GitHub request, so an
already verified update can apply offline.

The verifier bootstraps GitHub's trust repository from the root embedded in the
Orbit build, refreshes TUF metadata when its one-day cache expires, and fails the
check closed when current trusted material cannot be obtained. Root rotation is
accepted only through TUF. Once a candidate is staged, apply uses the recorded
verification result and does not refresh trust metadata.

Set the installation-wide preference from any default or named runtime:

```bash
orbit settings set automatic-updates off
```

Off disables automatic checks, downloads, and apply, including update-related
background network traffic. An explicit `orbit update` still performs a bounded
foreground check. Homebrew and Scoop installations are check-only and retain
the package-manager commands described above. Agent plugins remain independently
versioned and are updated by their host marketplace, not this mechanism.

### Rollback

```bash
orbit update --rollback
```

When an environment is running, rollback reconnects it and restores its prior
running resources just like a forward update.

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

### Orbit plugin

Install the Orbit plugin before the CLI when you want the
agent to guide first-time setup as well as operate an existing environment.

Claude Code:

```bash
claude plugin marketplace add iml885203/orbit
claude plugin install orbit@orbit
```

Codex CLI:

```bash
codex plugin marketplace add iml885203/orbit
codex plugin add orbit@orbit
```

Start a new agent session after installation. The bundled skill detects a
missing Orbit CLI, explains the platform-specific installer, and asks before
running it. Each source release also includes `plugins/orbit` for local
plugin development. The Codex and Claude manifests share one independently
released calendar version, so skill-only updates do not require a CLI release.

## Contributing to Orbit

### Testing unreleased main

Requires Go 1.25+, Node.js 22+, and pnpm 10+:

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
