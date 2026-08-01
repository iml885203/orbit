# Using Orbit under an E2E test suite

[English](./e2e-testing.md) · [繁體中文](./e2e-testing.zh-TW.md)

Orbit's job in an E2E setup is deliberately small: provide the shared
infrastructure, and answer questions about it truthfully. The test harness
layers everything test-specific on top and never reconfigures the machine.

## The layering pattern

Split the stack into three layers with different owners:

| Layer | Owner | Lifetime |
|---|---|---|
| Infrastructure (database, cache, broker) | Orbit (`orbit up --infra`) | long-lived, shared with everyday dev |
| Test data (a dedicated database or schema) | the test harness | disposable, reset between scenarios |
| System under test (the app runtime) | the test harness | started per run with a test profile |

The infrastructure a developer already runs is the same infrastructure the
suite uses. The suite does not start its own copy; it creates its own
database on the shared server, points its own app runtime at it, and cleans
that data through the application (a test-only reset endpoint is orders of
magnitude faster than any infrastructure-level restore).

## Verify the substrate — never switch it

`orbit switch` stops running resources and restarts the daemon. It is a
machine-wide provisioning step: correct in a pipeline's setup stage or typed
by a person, wrong as an implicit side effect of running tests. Orbit
enforces this — with resources running, `switch` asks for confirmation and a
non-interactive caller must pass `--yes`.

A harness should instead *verify* before it runs:

1. Read `orbit status --json`. It reports the active env's identity
   (`config_path`) and every resource's name, state, and ports.
2. Check that the resources the suite needs exist and are healthy.
3. If they are not, fail fast with an actionable message — name the missing
   resources and print the exact `orbit switch <env>` / `orbit up` command a
   human (or the pipeline's provisioning stage) should run. Do not run it
   yourself.

On failure, `orbit up --json` carries its own evidence: `data.failed_resources`
lists each resource that did not become healthy with its state, reason, and a
bounded log tail. A CI log gets the explanation without a second command. See
[the agent CLI contract](agent-cli.md) for the payload shapes.

## Running a second stack on one machine

For a dedicated runner — or a careful second stack beside your dev
environment — Orbit isolates everything through three environment variables,
the same mechanism its own test suite uses:

```bash
export ORBIT_HOME=/tmp/orbit-ci        # socket, state, env selection
export ORBIT_NAMESPACE=ci-$RANDOM      # container names + labels
export ORBIT_DASHBOARD_PORT=24500      # dashboard listener
```

Every command run with these set operates on a fully separate Orbit: its own
daemon, its own containers, its own state. One caveat: host ports are still
declared statically in the env file, so two stacks of the *same* env collide
on ports — give the second stack an env file with different host ports.
