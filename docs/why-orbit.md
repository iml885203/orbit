# Why Orbit?

[English](./why-orbit.md) · [繁體中文](./why-orbit.zh-TW.md)

Orbit is for local application environments that do not fit honestly into one
process or one container file. Application code often runs best on the host,
while databases, queues, caches, and supporting tools belong in containers.
Developers should not need a different control plane for each half.

## The user model

Orbit deliberately keeps the normal development loop small:

```text
orbit up       start the environment
orbit status   see what is actually ready
orbit logs <resource>
               inspect application output
orbit down     stop the environment
```

An `orbit.yaml` describes environment intent: resources, dependencies,
endpoints, and health. Orbit owns the mechanics behind that intent:

- start host processes and containers in dependency order;
- wait for real readiness instead of equating “process exists” with “ready”;
- inject the selected endpoints into dependents;
- keep logs and state available through a resident local daemon;
- reconnect the environment after Orbit or its config changes;
- expose the same state through the CLI, dashboard, and structured JSON.

The daemon is an implementation detail in the common path. `orbit up` starts
it when needed, applies valid config edits, and restores resources that were
already running.

## Start locally, share when it earns the cost

A project can begin with one `orbit.yaml` beside its code. There is no required
global workspace layout or environment repository. Once the environment is
useful to a team, the same file can move into a versioned Git repository and
be selected with `orbit switch`.

This separates two decisions that are often coupled unnecessarily:

1. prove that Orbit improves one project's local loop;
2. distribute a maintained environment to a team.

See [Use Orbit with your project](local-first.md) for that path.

## What Orbit replaces

Orbit is most valuable when a project has accumulated several partially
overlapping tools:

| Existing approach | What Orbit consolidates |
|---|---|
| Shell scripts or task runners | dependency ordering, long-lived state, health, logs, and recovery |
| Container-only orchestration | host-run application services and containers in one graph |
| Per-service terminals | one lifecycle with targeted start, stop, restart, and logs |
| Hand-maintained setup notes | executable readiness checks with a specific next action |
| Agent-specific automation | one versioned JSON contract shared by people and coding agents |

Orbit does not require replacing a project's build system, package manager, or
container images. Service commands remain the commands developers already use.

How these decisions relate to specific tools is tabled in
[Comparisons](#comparisons) at the end of this page.

## Infrastructure without a database worldview

Containers are generic resources. Health checks, dependency injection, seed
commands, and lifecycle behavior do not assume a database engine.

Orbit provides small conveniences for common container-native clients:

- `orbit query redis`
- `orbit query mongo`
- `orbit query postgres`
- `orbit exec <container> <client...>` for everything else

Seed behavior is also command-based: the environment declares the client
command and files to send to it. Orbit tracks what ran without interpreting
the data format.

SQL Server Database Projects have additional diff, publish, and reset semantics
that cannot be generalized honestly to every database. They therefore live in
an explicit optional extension. Its commands, checks, and dashboard page stay
hidden unless an environment enables `sqlserver`. The core lifecycle does not
depend on it.

## Designed for truthful recovery

The fastest command is not useful if it leaves the user uncertain about what
happened. Orbit treats recovery as part of the command contract:

- invalid config is rejected before a working environment is interrupted;
- occupied movable ports are resolved across the dependency graph;
- a failed dependency identifies the blocked resources and the next useful
  action;
- switching projects cannot silently control another project's resources;
- destructive database actions require explicit confirmation;
- JSON errors carry stable codes and executable recommended actions.

The goal is not to hide failures. It is to make the next correct action obvious.

## Under an E2E suite

The environment that serves daily development can also be the substrate under
an E2E test suite—on a developer's machine or a CI runner. Orbit's role stays
deliberately small: provide the shared infrastructure and answer questions
about it truthfully. The harness owns test data and the system under test; it
verifies the substrate through `orbit env info --json` instead of
reprovisioning it, and fails fast with an executable message when a required
resource is missing.

See [Using Orbit under an E2E test suite](e2e-testing.md) for the layering
pattern.

## Where Orbit stops

Orbit is intentionally not:

- a production deployment platform;
- a package manager or runtime installer;
- a multi-user remote control service;
- a universal database schema framework;
- a replacement for application-level service discovery or telemetry SDKs.

It is a single-user, single-machine control plane for development and test
environments. It binds its dashboard to loopback, leaves project toolchains under the developer's control, and focuses
on shortening the inner loop without changing how the application is deployed.

Those boundaries are part of the product: fewer promises mean a smaller mental
model and more predictable behavior inside the scope Orbit owns.

## Comparisons

Design decisions, not feature lists. Other tools as publicly documented in
August 2026.

### Orbit and Docker Compose

|  | Compose | Orbit |
|---|---|---|
| Runs | containers | host processes + containers, one graph |
| "Ready" means | container started | endpoint answers |
| Between commands | — | resident daemon keeps state, logs, health |
| Machine interface | — | versioned `orbit.cli.v1` JSON contract |

**Choose Compose** when the whole stack is containerized and one compose file
serves both dev and deployment.

### Orbit and Aspire

| Decision | Aspire | Orbit |
|---|---|---|
| App model | code-first AppHost (C# / TypeScript) | one declarative `orbit.yaml` |
| Environment lifetime | while `aspire run` runs | resident daemon, outlives sessions |
| Switching scenarios (shared infra) | restart the AppHost and everything it runs | shared containers keep running |
| Scope | dev → deployment (`publish` / `deploy`) | stops before deployment |
| Distribution unit | AppHost inside the solution | YAML in any Git repo, can span repos |

**Choose Aspire** for environment-as-code, typed integrations, an
OpenTelemetry dashboard, and one model from dev to deployment—especially on
Azure.

**Choose Orbit** for one reviewable file, an environment that outlives the
terminal, multi-repo stacks, and a stable JSON contract for coding agents and
test harnesses.

### SQL Server schema management

|  | Aspire (Community Toolkit) | Orbit (`sqlserver` extension) |
|---|---|---|
| Apply schema | auto-publish dacpac at startup; dashboard "Redeploy" | `orbit sqlserver publish`, data-preserving |
| Preview changes | — | `orbit sqlserver diff` |
| Clean reset | — | `orbit sqlserver reset`, requires confirmation |
| Query | — | `orbit sqlserver query` |
