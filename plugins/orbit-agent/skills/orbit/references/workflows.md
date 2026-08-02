# Orbit workflows

## Normal entry

```bash
orbit inspect --json
```

Inspect is the single agent entry point. It reports readiness, selected
environment, resource state, risks, and the next useful actions. Run
`orbit doctor --json` only when inspect identifies setup or runtime
prerequisites. Use `orbit history --json` only for an audit trail.
On a fresh install, execute inspect's exact setup action
(`orbit init --yes --json`) directly; do not insert doctor first.

## Start and stop

```bash
orbit up --json
orbit up <resource> --json
orbit restart <resource> --json
orbit down <resource> --json
orbit down <resource> <resource> --json
orbit down --group <name> --json
orbit down --infra --json
orbit down --json
```

Use the narrowest command that achieves the requested change. Verify state
afterward with `orbit status --json`. `orbit up` automatically starts Orbit's
daemon and converges pending config edits, so neither daemon management nor a
separate apply step belongs in the normal startup path.

Use `orbit up --infra --json` only when the user explicitly requests
containers without host services.

## Environments

```bash
orbit env sync --json
orbit switch <name> --json
orbit env list --json
orbit env info --json
```

Sync applies changed active configuration by default while restoring the
resources that were running; resources that were stopped remain stopped.
`--no-apply` deliberately defers that interruption. A switch handles the daemon
handoff itself; follow its response rather than restarting Orbit preemptively.
With resources running, switch first returns `confirmation_required`; rerun
with `--yes` only when stopping them is what the user wants. `env info`
reports each resource's ports and URL with declared/observed provenance —
read it before connecting anything from outside the stack.

For a project-local `orbit.yaml`, edit the file and run `orbit up --json`.
Use `orbit env apply --json` only when the config must change while every
resource that was stopped remains stopped.

## Failures

Follow the relevant JSON `recommended_actions` entry. Do not insert
an intermediate `status`, another `inspect`, `doctor`, or a daemon command
unless the response asks for it. The one final `orbit status --json`
verification still applies after a state change.

For a failed resource, reuse the initial inspect snapshot and read
`orbit logs <resource> --json`. After fixing the cause, execute the response's
targeted retry action; do not choose a broader command from prose. Dependencies
recover through normal lifecycle behavior.

## Database workflow

Native Redis, MongoDB, and PostgreSQL query commands use the client inside the
configured container. Their interactive/native output is not an
`orbit.cli.v1` envelope.

The SQL Server Database Projects workflow is optional and exists only when the
environment enables it. Use `orbit sqlserver diff <database> --json` to inspect
schema impact. Use `orbit sqlserver publish <database|project> --json` (or
`--all --json`) for the ordinary data-preserving path; changes that could lose
data remain blocked. Ask before:

- `orbit sqlserver reset`
- `orbit sqlserver publish --allow-data-loss`

State the database target and expected data impact in the confirmation. A
forced publish requested with `--json` does not run; its envelope returns one
`destructive: true` manual command that preserves `--allow-data-loss` and the selected
scope. Execute that exact command only after approval; Orbit asks for
confirmation again before publishing.

## Tunnels

Claim only the narrowest callback path needed. Confirm the target is a
development or authorized staging environment, and release the claim when the
task ends. Callback bodies may contain credentials or personal data; do not
copy them into reports.
