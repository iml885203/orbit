# Orbit — Agent Instructions

Orbit is a team-neutral local-dev orchestrator (Go CLI + daemon + Svelte UI). This single repo contains the neutral engine, CLI, daemon, and UI, plus the feature packages `internal/devdb` (SQL schema workflow) and `internal/tunnel` (staging callback tunnel), surfaced in the dashboard via `ui/src/lib/devdb` and `ui/src/lib/tunnel`. For project context, read [README.md](README.md) and the structured docs under [docs/](docs/).

This file is agent-specific guidance. Project conventions live in [docs/CODE_CONVENTIONS.md](docs/CODE_CONVENTIONS.md) and `.claude/rules/`.

## Boundaries

### Always
- Use `--json` when parsing CLI output. Human output is not a stable contract. For the full agent-facing JSON contract (`orbit.cli.v1` envelope, structured errors, converted commands, NDJSON log streaming, legacy JSON exceptions), read [docs/agent-cli.md](docs/agent-cli.md).
- Run `make preflight` (the full CI gate: build, tests, vet, verify-types) before **each commit** — not just at the end. The gotcha it exists to catch: after changing any Go struct or config field — even a doc comment tygo emits — run `make gen-types` and stage the result, or `verify-types` fails on the drifted `ui/src/lib/types/*.gen.ts`. `make lint` for the stricter golangci pass.
- Read [docs/CODE_CONVENTIONS.md](docs/CODE_CONVENTIONS.md) before non-trivial edits.
- Walk the [Definition of Done checklist](docs/CODE_CONVENTIONS.md#19-definition-of-done--pre-commit-checklist) before each commit — it covers the judgment calls `make preflight` can't check (dead files/docs, intent-revealing refactor, behavioral test coverage).

### Ask first
- Editing `~/.orbit/settings.json` directly — use the UI/CLI path instead.
- Destructive ops: `docker volume rm`, `orbit sqlserver reset` (returns one DB to a clean latest-schema state, discarding local data changes), `orbit sqlserver publish --force` (allows data-loss schema changes). The former image-build flow and container-side apply have been removed; publish/reset run entirely on the host.

### Never
- `git push --force`, `git commit --no-verify`.
- The convention hard-lines — WHAT-comments (§2), cross-domain helpers in `utils.go`/`shared/` (§4), thin service interfaces (§6), error classification via `strings.Contains(err.Error(), ...)` (§9). Details and rationale in [docs/CODE_CONVENTIONS.md](docs/CODE_CONVENTIONS.md); the matching `.claude/rules/` files enforce them per file type.

## Rules

Project conventions live in `.claude/rules/`. Each file has `paths:` frontmatter declaring when it applies.

- **Claude Code**: rules auto-load when you Read a matching file.
- **Codex CLI / other agents**: read them explicitly before editing:
  - Go files (`*.go`): `.claude/rules/error-handling.md`, `.claude/rules/go-*.md`, `.claude/rules/domain-organization.md`
  - Svelte / TS files: `.claude/rules/svelte-*.md`, `.claude/rules/domain-organization.md`
- UI design changes: read [DESIGN.md](DESIGN.md) first; it is the agent-facing source for Orbit's dashboard visual system.

Long-form rationale and examples live in [docs/CODE_CONVENTIONS.md](docs/CODE_CONVENTIONS.md).

## CLI gotchas

Discover commands with `orbit --help`; the agent-facing JSON contract is in [docs/agent-cli.md](docs/agent-cli.md). Non-obvious behaviors:

- If `orbit up` errors about missing envs, run `orbit init` or `orbit env sync`.
- `orbit up` auto-starts the daemon; no separate `orbit daemon start` needed.

## Code review

- `/orbit-review` — review unstaged + staged changes
- `/orbit-review <base>` — review branch vs base (e.g. `/orbit-review main`)

## Where to read more

- [docs/architecture.md](docs/architecture.md) — state machine and event model
- [docs/troubleshooting.md](docs/troubleshooting.md) — runtime and optional DB-workflow errors
