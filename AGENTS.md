# Orbit — Agent Instructions

Orbit is a team-neutral local-dev orchestrator (Go CLI + daemon + Svelte UI). This single repo contains the neutral engine, CLI, daemon, and UI, plus the gate-scanned feature packages `internal/devdb` (SQL schema workflow) and `internal/tunnel` (staging callback tunnel), surfaced in the dashboard via `ui/src/lib/devdb` and `ui/src/lib/tunnel`. For project context, read [README.md](README.md) and the structured docs under [docs/](docs/).

This file is agent-specific guidance. Project conventions live in [docs/CODE_CONVENTIONS.md](docs/CODE_CONVENTIONS.md) and `.claude/rules/`.

## Boundaries

### Always
- Use `--json` when parsing CLI output. Human output is not a stable contract. For the full agent-facing JSON contract (`orbit.cli.v1` envelope, structured errors, converted commands, NDJSON log streaming, legacy JSON exceptions), read [docs/agent-cli.md](docs/agent-cli.md).
- Run `make preflight` (the full CI gate: build, tests, vet, verify-types, check-neutral) before **each commit** — not just at the end. Two gotchas it exists to catch: (1) after changing any Go struct or config field — even a doc comment tygo emits — run `make gen-types` and stage the result, or `verify-types` fails on the drifted `ui/src/lib/types/*.gen.ts`; (2) `check-neutral` rejects any brand/team name ("example", "dbproject", …) across the tree, including the gate-scanned feature packages `internal/devdb` and `internal/tunnel` — keep it all neutral (permitted exceptions are enumerated in `scripts/check-neutral.sh`). `make lint` for the stricter golangci pass.
- Read [docs/CODE_CONVENTIONS.md](docs/CODE_CONVENTIONS.md) before non-trivial edits.

### Ask first
- Editing `~/.orbit/settings.json` directly — use the UI/CLI path instead.
- Destructive ops: `docker volume rm`, `orbit sqlserver reset` (returns one DB to a clean latest-schema state, discarding local data changes), `orbit sqlserver publish --force` (allows data-loss schema changes). The former image-build flow and container-side apply have been removed; publish/reset run entirely on the host.

### Never
- `git push --force`, `git commit --no-verify`.
- WHAT-comments. Comments answer WHY only. (CODE_CONVENTIONS §2)
- Service indirection / thin interfaces. (CODE_CONVENTIONS §6)
- Cross-domain helpers in `utils.go` / `shared/`. (CODE_CONVENTIONS §4)
- `strings.Contains(err.Error(), ...)` for error classification in production code. (CODE_CONVENTIONS §9)

## Rules

Project conventions live in `.claude/rules/`. Each file has `paths:` frontmatter declaring when it applies.

- **Claude Code**: rules auto-load when you Read a matching file.
- **Codex CLI / other agents**: read them explicitly before editing:
  - Go files (`*.go`): `.claude/rules/error-handling.md`, `.claude/rules/go-*.md`, `.claude/rules/domain-organization.md`
  - Svelte / TS files: `.claude/rules/svelte-*.md`, `.claude/rules/domain-organization.md`
- UI design changes: read [DESIGN.md](DESIGN.md) first; it is the agent-facing source for Orbit's dashboard visual system.

Long-form rationale and examples live in [docs/CODE_CONVENTIONS.md](docs/CODE_CONVENTIONS.md).

## CLI workflows

```bash
orbit up --infra              # start containers
orbit up                      # start services
orbit status --json           # verify state
orbit switch <env>            # change env and restart daemon
orbit logs <service>          # plaintext; -f to stream
orbit doctor                  # checks setup; prints fixes
orbit sqlserver query "SELECT TOP 5 * FROM Users"
orbit sqlserver publish <dbname> # fast dev-loop, preserves data
```

If `orbit up` errors about missing envs, run `orbit init` or `orbit env sync`.
`orbit up` auto-starts the daemon; no need for separate `orbit daemon start`.

## Code review

- `/orbit-review` — review unstaged + staged changes
- `/orbit-review <base>` — review branch vs base (e.g. `/orbit-review main`)

## Where to read more

- [README.md](README.md) — entry point
- [docs/CODE_CONVENTIONS.md](docs/CODE_CONVENTIONS.md) — full coding standards
- [architecture.md](docs/architecture.md) — state machine and event model
- [docs/troubleshooting.md](docs/troubleshooting.md) — runtime and optional DB-workflow errors
