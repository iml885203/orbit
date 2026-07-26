# Orbit workflows

## Inspect

```bash
orbit status --json
orbit doctor --json
orbit inspect --json
orbit history --json
```

## Start and stop

```bash
orbit up --infra --json
orbit up --json
orbit service restart <name> --json
orbit down --json
```

Use the narrowest command that achieves the requested change. Verify state
afterward with `orbit status --json`.

## Environments

```bash
orbit env sync --json
orbit switch <name> --json
orbit env list --json
```

An environment switch may require a daemon restart. Follow the response's
recommended action rather than restarting preemptively.

## Database workflow

`orbit db diff` and `orbit db publish` preserve data by default. Ask before:

- `orbit db reset`
- `orbit db publish --clean`
- `orbit db publish --force`

State the database target and expected data impact in the confirmation.

## Tunnels

Claim only the narrowest callback path needed. Confirm the target is a
development or authorized staging environment, and release the claim when the
task ends. Callback bodies may contain credentials or personal data; do not
copy them into reports.
