# Comparisons

[English](./comparisons.md) · [繁體中文](./comparisons.zh-TW.md)

Design decisions, not feature lists. Other tools as publicly documented in
August 2026.

## Orbit and Docker Compose

|  | Compose | Orbit |
|---|---|---|
| Runs | containers | host processes + containers, one graph |
| "Ready" means | container started | endpoint answers |
| Between commands | — | resident daemon keeps state, logs, health |
| Machine interface | — | versioned `orbit.cli.v1` JSON contract |

**Choose Compose** when the whole stack is containerized and one compose file
serves both dev and deployment.

## Orbit and Aspire

| Decision | Aspire | Orbit |
|---|---|---|
| App model | code-first AppHost (C# / TypeScript) | one declarative `orbit.yaml` |
| Environment lifetime | while `aspire run` runs | resident daemon, outlives sessions |
| Scope | dev → deployment (`publish` / `deploy`) | stops before deployment |
| Distribution unit | AppHost inside the solution | YAML in any Git repo, can span repos |

**Choose Aspire** for environment-as-code, typed integrations, an
OpenTelemetry dashboard, and one model from dev to deployment—especially on
Azure.

**Choose Orbit** for one reviewable file, an environment that outlives the
terminal, multi-repo stacks, and a stable JSON contract for coding agents and
test harnesses.
