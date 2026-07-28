# Versioning and compatibility

Orbit and the bundled `plugins/orbit-agent` plugin use the same
[Semantic Versioning](https://semver.org/) release number.

## Release sequence

- Private GitHub rehearsal releases began at `v0.0.1`.
- The source repository became public during the pre-1.0 product-hardening
  phase. All `0.x` releases are previews and may contain breaking changes.
- The first stable release is `v1.0.0`. Publishing that tag means the
  compatibility contracts below are documented, tested, and ready for external
  users.
- Release tags are immutable. Fixes are published as a new version.

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

Every release updates both plugin manifests to the Orbit release number:

- `plugins/orbit-agent/.codex-plugin/plugin.json`
- `plugins/orbit-agent/.claude-plugin/plugin.json`

The plugin may use only commands and contracts available in the same Orbit
version. A release is incomplete if either manifest differs from the Orbit tag.
