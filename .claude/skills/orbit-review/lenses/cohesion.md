# Lens: cohesion

You are looking for domain organisation problems and weak naming.

## Catch

- **File name doesn't answer "what domain?"**: `utils.go`, `helpers.go`, `services.go`, `lib/utils.ts`, `lib/helpers.ts`.
- **Helper used by multiple files but defined in only one of them**: should move to a domain-named file.
- **File >800 lines** (hard limit per CODE_CONVENTIONS §4).
- **Function >50 lines** (per CODE_CONVENTIONS).
- **Nesting >4 levels deep**.
- **Naming that doesn't carry intent**: `func processData()`, `var temp`, `var result1`. Rename until the code speaks.
- **WHAT-comments**: `// loop over services` above `for _, s := range services`. Delete or refactor.
- **Mutable state without documented lock precondition** (Go, per §10).
- **Event loop without documented drop policy** (Go, per §12).
- **Mixed-domain files**: one file handling two unrelated concerns. Split.

## Don't catch

- WHY-comments explaining non-obvious rationale, constraints, or trade-offs. Those belong.
- Files in the 400–800 line range that own a single domain coherently.
- Long type definitions or large data tables.

## Severity guidance

- **CRITICAL**: violates CODE_CONVENTIONS §4 or §10 (named rules) directly.
- **SHOULD_FIX**: smell against cohesion or naming, not a §-rule violation.
- **CONSIDER**: minor naming preferences.

## Reference rules

- `.claude/rules/domain-organization.md`
- `.claude/rules/go-mutability.md`
- `.claude/rules/go-event-loop.md`
- `docs/CODE_CONVENTIONS.md` §1, §2, §4, §10, §12
