# Lens: reuse

You are looking for duplicated logic, bypassed utilities, and reinvented adjacent patterns.

## Catch

- **Same logic appearing 2+ times**: extract or accept as conscious duplication (three similar lines is sometimes fine; document if intentional).
- **Existing utility bypassed**: `commandString()`, `effectiveTimeout()`, `waitForLifecycleJSON()`, `apiPost()`, etc. Reviewer should know orbit's vocabulary.
- **Adjacent domain pattern reinvented**: if `runDown` already establishes a flow, `runStop` doing the same flow differently is suspicious.
- **Test setup duplicated**: a `e2eEnv` helper exists — new tests bypassing it.
- **New ad-hoc `strings.Contains(err.Error(), ...)`** classifications instead of sentinel/typed errors. (§9 violation; pair with `error-handling` rule.)
- **Multiple `recommendedActions` builders maintaining their own dedup logic** — should consolidate.
- **Wire types reinvented**: if `daemon.StatusResponse` already encodes a service shape, don't define a new one in the CLI.

## Don't catch

- Intentional shallow duplication for clarity (e.g. two similar test cases each fully spelt out).
- Generalisation beyond what's used (YAGNI; lens `simple-design` covers this).

## Severity guidance

- **CRITICAL**: introduces a new `strings.Contains(err.Error(), ...)` classification (§9), or bypasses a documented utility.
- **SHOULD_FIX**: 2+ duplications of logic with non-trivial size.
- **CONSIDER**: minor copy-paste of trivial code.

## Reference rules

- `.claude/rules/error-handling.md`
- `.claude/rules/domain-organization.md`
- `docs/CODE_CONVENTIONS.md` §9
