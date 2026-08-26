# Versioning and compatibility

Orbit CLI uses [Semantic Versioning](https://semver.org/). Its candidate
version lives in the root `VERSION` file. The bundled `plugins/orbit` plugin
versions independently so skill-only changes do not force a CLI release.

## Release sequence

- Private GitHub rehearsal releases began at `v0.0.1`.
- The source repository became public during the pre-1.0 product-hardening
  phase. All `0.x` releases are previews and may contain breaking changes.
- GitHub titles `0.x` releases as `Orbit vX.Y.Z (Preview)`. They remain the
  repository's installable latest release so the default installer and
  `orbit update` always resolve the newest supported preview without requiring
  users to choose a release channel.
- The first stable release is `v1.0.0`. Publishing that tag means the
  compatibility contracts below are documented, tested, and ready for external
  users. [The 1.0 test matrix](1.0-test-matrix.md) holds the platform evidence
  that tag requires and records what has actually been exercised.
- Published GitHub releases are immutable: their tag and assets cannot be
  changed or reused. Fixes are published as a new version.

## Preview batching

Commits on `main` are unreleased work and may accumulate across several related
fixes. A preview is cut only when the batch delivers one coherent user outcome
that can be stated and verified as an installed-user journey. A passing commit
or one corrected edge case is not, by itself, a release boundary.

Freeze a preview batch in this order:

1. Complete the related implementation and its strongest practical journey.
2. Review the combined user-visible difference from the previous release.
3. Choose the next version and update `VERSION`. Do not create the Orbit tag
   yet.
4. Prepare and review the user-facing release notes.
5. Run the candidate and platform gates, then manually approve publication.
   The release workflow creates the Orbit tag only after every gate passes, so
   a failed candidate never leaves a release tag behind. GitHub locks the tag
   and complete asset set when the release is published, then emits an
   immutable-release attestation covering their digests and target commit.
6. The workflow verifies that release attestation and every published asset
   before either package repository can be updated. Homebrew and Scoop repeat
   the same read-only verification before their write-capable update jobs.

Release notes are entered when the release workflow is approved and live in
GitHub Releases rather than accumulating in the source tree. They describe the
batch's user outcome first; individual fixes are supporting details.

## The demo repository versions itself

`orbit init` clones [orbit-demo](https://github.com/iml885203/orbit-demo) at the
ref pinned by `EnvRepoRef` in `internal/distribution`, so that ref must always
exist — `make release-check` fails if it does not.

The demo is **not** re-tagged for each Orbit release. It uses calendar
versioning, `vYEAR.MONTH.N` where `N` counts releases within that month
(`v2026.8.1` is August's first), and it is tagged only when the demo itself
changes. Bump `EnvRepoRef` in the same commit that adopts a new demo tag, which
is a demo-driven change rather than a step in cutting an Orbit release.

Repository automation reads these compile-time values through
`make distribution-metadata`. Its `orbit.distribution.v1` JSON schema is a
maintainer contract independent of the Orbit release version; runtime code
continues to consume the Go declaration directly. The standalone installers
keep their repository default inline so `curl | sh` and `irm | iex` remain
self-contained, and `make test-distribution-metadata` checks those bootstrap
defaults against the exported metadata.

Sharing Orbit's version number was the earlier scheme. It forced an empty demo
tag per Orbit release whose only content was a pairing declaration, so the two
now version independently.

Package updates use the private `iml885203-package-sync` GitHub App. Install
the App only on `iml885203/homebrew-tap` and `iml885203/scoop-bucket` with
`Actions: Read and write` and `Contents: Read`. Configure its client ID as the
`PACKAGE_SYNC_APP_CLIENT_ID` repository variable and its private key as the
`PACKAGE_SYNC_APP_PRIVATE_KEY` repository secret.

Immutable Releases is an owner-managed repository prerequisite. The workflow
does not hold repository-administration credentials: after publication it
fails closed if GitHub does not report an immutable, attested release, which
prevents package promotion but cannot retroactively make a mutable publication
immutable. Build-provenance and SBOM attestations remain separate evidence of
how the binaries were produced; the immutable-release attestation binds the
published tag, target commit, and release assets.

Official direct-install updates independently consume that release attestation
in process. They bind the selected platform binary and `checksums.txt` to the
attested digests before staging, retain bounded evidence for offline delayed
apply, and revalidate the staged bytes immediately before replacement. This
runtime boundary is separate from publication verification and build provenance.

Pre-1.0 releases may introduce breaking changes. From `v1.0.0` onward:

- PATCH releases contain backward-compatible fixes.
- MINOR releases add backward-compatible functionality.
- MAJOR releases may change a stable contract incompatibly.

## Stable contracts from v1

The following surfaces are compatibility contracts:

- CLI command names, flags, exit behavior, and documented human workflows;
- agent-facing JSON envelopes and their named schema versions;
- environment YAML schema and validation behavior;
- persisted user settings;
- daemon HTTP API;
- public extension API.

Additive changes are allowed in MINOR releases. Consumers of JSON and HTTP
responses must ignore unknown fields. Removing or renaming a field, command,
flag, configuration key, or public Go symbol requires either a new named schema
version or a MAJOR Orbit release.

Undocumented internals, dashboard markup and styling, log wording, and test
fixtures are not compatibility contracts.

## Plugin version

The plugin uses `YEAR.MONTH.N`, where `N` counts plugin releases within that
month (`2026.8.1` is August 2026's first). Update both manifests together:

- `plugins/orbit/.codex-plugin/plugin.json`
- `plugins/orbit/.claude-plugin/plugin.json`

The two manifests must match each other, but do not match the Orbit CLI
version. Increment the plugin version only when plugin contents change. The
plugin must detect the installed CLI and avoid teaching commands or contracts
that unavailable supported CLI versions cannot provide.
