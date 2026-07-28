# Why Orbit? — the comparison behind the claims

[English](./why-orbit.md) · [繁體中文](./why-orbit.zh-TW.md)

Every claim in the README's "Why Orbit?" section traces back to a measured
comparison we ran in July 2026: our real dev environment (5 containers, a
.NET backend, a pnpm frontend) ported to .NET Aspire 13.4 and driven through
a full development loop — DB schema change, backend API change, frontend
codegen — on the same machine, side by side with Orbit. This doc records the
data, including where Aspire wins.

## The structural difference

Aspire's model is **environment-as-code**: the AppHost is a compiled C#
program, and that program *is* the orchestrator. Orbit's model is
**environment-as-configuration plus a resident control plane**: envs are
YAML, and a long-lived daemon executes verbs against them.

Environment-as-code is a genuine advantage in the outer loop — typed,
reviewable, and (with `aspire do`) deployable from the same model. In the
inner loop it becomes a tax, because changing the environment means
rebuilding and restarting the orchestrator itself. The boundary, measured:

| Inner-loop action | Aspire 13.4 | Orbit |
|---|---|---|
| Edit your service's code | ✅ restart one resource (~36s, dominated by `dotnet` rebuild) | ✅ equivalent |
| Change an env var / connection string | ❌ edit C# → restart the AppHost world | edit YAML → restart that one service |
| Add/remove a service | ❌ AppHost restart; session containers die with it | edit YAML → daemon restart **reconnects** running services |
| Start a subset of resources ad hoc | ❌ requires `WithExplicitStart` written in advance | `orbit up <resource>` or `orbit up --group <group>` at runtime |
| Switch environments | ❌ no env primitive — stop world + AppHost rebuild (15–25s) + cold start | `orbit switch`: seconds; shared infra keeps running |
| Apply a DB schema change | ❌ not covered: `dotnet build` + `sqlpackage` (8 flags) by hand | `orbit db publish` — one verb |

Aspire's own issue tracker corroborates the diagnosis: the team's proposal
to decouple DCP and the dashboard from the AppHost (so a restart doesn't
tear everything down) is exactly a move toward the resident-control-plane
shape, and "running subsets of services" is on their roadmap as a known gap.

## Measured results (July 2026, Apple Silicon, warm images)

- **Aspire runs a real stack fine.** Our 257-line env YAML ported to ~120
  lines of AppHost C# and worked first try: 76s from `aspire start` to
  backend health + frontend serving. "Can it run the stack" is *not* a
  differentiator — don't let anyone tell you otherwise.
- **The DB loop is the widest gap.** One table added to a real SSDT project:
  Aspire world took 3 tools and 2 traps (an MSB3030 referenced-project
  failure whose fix — `dotnet build -o` — is tacit knowledge, then
  `sqlpackage` with 8 flags), ~24s machine time, with Aspire itself absent
  from the loop. Orbit: one idempotent command, 8.6s median over a month of
  real usage. Honest footnote: Aspire's no-op redeploy (1.1s) is faster
  than Orbit's (6.4s, full `sqlpackage` diff every time).
- **`orbit db reset` has no equivalent anywhere in the compared field.**
  0.9s median (database snapshot revert) vs minutes of volume-delete +
  re-bootstrap + re-seed.
- **Backend/frontend iteration is a tie.** Resource restart (~36s) and
  codegen (~4s) are the same in both worlds. Not a selling point.
- **Process cleanup is a tie in graceful stops — and Aspire wins the crash
  case.** Both leave zero orphans on a normal stop. `kill -9` the
  orchestrator and Aspire's DCP still cleans up; Orbit's process-group kill
  needs the daemon alive, so orphans linger until the next daemon start
  reconnects and adopts them. We report this against ourselves because the
  rest of this doc deserves your trust.
- **Agent surface.** Aspire's MCP server (13.4) exposes 14 tools: strong
  observation (resources, logs, traces, docs), one control verb
  (`execute_resource_command`), no stack lifecycle, env, seed, or DB verbs,
  and it requires a running AppHost plus project context. Orbit's contract
  is a global binary with structured JSON errors that carry the recommended
  next command. An agent on Aspire can *look* and *push per-resource
  buttons*; an agent on Orbit can *manage the environment*.

## What the time difference is worth

Frequencies below are not estimates — they come from one developer's real
30-day command history (1,537 commands). Machine-time deltas are measured;
the ~30s/occurrence human overhead of multi-tool workflows is an estimate,
flagged as such.

| Operation | 30-day count | Per-occurrence delta | Monthly cost of the gap |
|---|---|---|---|
| Schema iterate (publish + diff) | 176 | ~45s | ~2.2 h |
| Clean-data reset | 32 | ~6 min (no Aspire equivalent) | ~3.2 h |
| Env switch | 22 | ~90s | ~0.6 h |
| Partial startup | 77 | ~47s | ~1.0 h |

**≈ 7 hours per developer per month**, nearly half of it from `db reset`
alone. Caveats, honestly: n=1, and that developer drives Orbit through
coding agents, which raises operation frequency — but that is the point.
Agent-driven development multiplies how often the environment gets touched,
which multiplies the per-touch gap. Halve the human-overhead estimate and
the figure is still ~5 hours.

DB operations were also the single most frequent command family in that
history (250 of 1,537 commands), and 94% of all commands came through the
CLI rather than the dashboard — the workflow claims above describe how the
tool is actually used, not how we wish it were used.

## Where Aspire wins

Use Aspire if these outweigh the inner loop for your team:

- **Telemetry breadth**: structured logs, metrics, and trace tooling in one
  dashboard. Orbit has in-memory traces only.
- **Typed .NET integration**: AppHost-level service discovery and client
  integrations reach into your application code; Orbit stops at YAML and
  env vars.
- **Deployment**: `aspire do` carries the same model to Azure/AKS/Helm.
  Orbit is local-only by design.
- **Ecosystem**: Microsoft-backed, large integration catalog, public
  community.

## Method notes

Single machine, single run for wall-clock numbers unless a median is
stated; medians come from 30 days of recorded usage. The Aspire port reused
Orbit's data volumes (so numbers exclude first-time database bootstrap) and
skipped sidecar UI containers. Aspire's SQL Database Projects community
toolkit was evaluated but binds to Aspire-managed database resources and
does not cover publishing to an existing server. Versions: Aspire CLI
13.4.6, `CommunityToolkit.Aspire.Hosting.SqlDatabaseProjects` 13.4.1-beta,
Orbit at commit `d6d0fcc`.
