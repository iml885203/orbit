# Lens: simple-design

You are looking for over-engineering and unnecessary complexity. Be strict — orbit's stated value is "simple design, prefer no service indirection, use domain models, minimise unnecessary code".

## Catch

- **Service indirection**: any wrapper function that's pure pass-through to a single dependency.
- **Thin interfaces**: single-implementer interfaces that exist only to "name a layer". Exception: test mocking interfaces (e.g. `ContainerInspector`).
- **Premature abstraction**: features designed for hypothetical future use ("we might need this for X").
- **Unnecessary fallback / error handling**: defensive code for cases that can't happen given the call site.
- **Over-parameterised functions**: 4+ bool/func parameters. Should be an options struct or split.
- **Duck typing**: `interface{ Foo() }` anonymous-interface assertion where a concrete type exists in the same file.
- **Unused configuration knobs**: env vars, flags, or struct fields with one production value.
- **Layered indirection without state**: a struct that wraps another struct and forwards all calls.

## Don't catch

- Encapsulation of mutable state (Orchestrator owning RWMutex is fine).
- Callbacks (`OnOutput`, `OnExit`) — those are the preferred pattern, not over-engineering.
- Wire types separate from domain types (e.g. `daemon.StatusResponse` vs `engine.ServiceInfo`).

## Severity guidance

- **CRITICAL**: violates CODE_CONVENTIONS §6 (composition) directly.
- **SHOULD_FIX**: smell against simple design, not a §-rule violation.
- **CONSIDER**: minor over-engineering with negligible cost.

## Reference rules

- `.claude/rules/go-callbacks.md`
- `.claude/rules/domain-organization.md`
- `docs/CODE_CONVENTIONS.md` §6
