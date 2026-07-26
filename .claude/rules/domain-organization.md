---
paths: ["**/*.go", "**/*.svelte", "**/*.ts"]
---

# Domain organisation

**Rule**: File names answer "what domain?" not "what layer?". No `utils.go`, `helpers.go`, `services.go`, `lib/utils.ts`. Helpers used by multiple files live in a domain-named file.

**Why**: Domain names tell readers what each file owns. Layer names ("handlers", "utils") tell you nothing about the code's purpose and become magnets for unrelated code.

**Good** (Go):
```
internal/daemon/handlers_settings.go      ← owns the settings HTTP domain
internal/daemon/handlers_graph.go         ← owns the graph HTTP domain
internal/engine/dep_graph.go              ← owns dep-graph logic
```

**Good** (TS):
```
ui/src/lib/graphActions.ts                ← graph manipulation
ui/src/lib/logClassify.ts                 ← log classification
ui/src/lib/traceColor.ts                  ← trace colouring
```

**Bad**:
```
internal/services/service.go              ← what kind of service?
cmd/orbit/helpers.go                      ← what kind of helper?
ui/src/lib/utils.ts                       ← magnet for unrelated code
```

**Helper placement**: If a function is called by `up.go`, `down.go`, and `restart.go`, it belongs in a shared file named for the concept it owns (e.g. `lifecycle.go`) — not in any of the three callers. The shared concept gets its own file.

**Exceptions**: `internal/<package>/<package>.go` (entry point of the package) and `_test.go` files are fine.

**See also**: `docs/CODE_CONVENTIONS.md` §4.
