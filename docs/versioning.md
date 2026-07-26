# Versioning and compatibility

Orbit and the bundled `plugins/orbit-agent` plugin use the same
[Semantic Versioning](https://semver.org/) release number.

## Release sequence

- Private GitHub rehearsal releases begin at `v0.0.1`.
- The first public release is `v1.0.0`. Publishing that tag means the
  compatibility contracts below are documented, tested, and ready for external
  users.
- Release tags are immutable. Fixes are published as a new version.

The GitHub-private phase may introduce breaking changes between `0.0.x`
releases. From `v1.0.0` onward:

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
