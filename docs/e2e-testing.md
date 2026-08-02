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

1. Read `orbit env info --json`. It reports the env's identity and, per
   resource, ports and URL with provenance — `declared` from the environment
   file, `observed` from the running daemon. Observed values are withheld when
   the daemon serves a different environment, so a harness can never mistake
   another stack's ports for its own. Credentials-bearing environment values
   require `--show-secrets`.
2. Check that the resources the suite needs exist and are healthy
   (`orbit status --json` has the full lifecycle detail).
3. If they are not, fail fast with an actionable message — name the missing
   resources and print the exact `orbit switch <env>` / `orbit up` command a
   human (or the pipeline's provisioning stage) should run. Do not run it
   yourself.

On failure, `orbit up --json` carries its own evidence: `data.failed_resources`
lists each resource that did not become healthy with its state, reason, and a
bounded log tail. A CI log gets the explanation without a second command. See
[the agent CLI contract](agent-cli.md) for the payload shapes.

## Running a second stack on one machine

For a dedicated runner or a second stack beside a development environment,
give the run a unique named instance:

```bash
orbit up --instance ci-$BUILD_ID --infra --json
orbit status --instance ci-$BUILD_ID --json
# run tests against the resolved endpoints returned above
orbit instance clean ci-$BUILD_ID --json
```

Named instances isolate daemon state, Docker resources, volumes, networks, and
host ports. Declared ports are preferences; the JSON responses report the
resolved endpoints, so the harness must consume them instead of assuming the
ports in the environment file. Keep the same `--instance` target for every
Orbit command in the run, and clean it after the suite finishes. See
[Isolated runtime instances](instances.md) for the ownership model.
