# Code conventions

[English](./CODE_CONVENTIONS.md) · [繁體中文](./CODE_CONVENTIONS.zh-TW.md)

This document captures the principles that guide structure and style decisions in Orbit. It exists so future contributors make the same calls without having to reverse-engineer the refactor history.

---

## 1. Philosophy

### Organise by domain, not layer

Code is grouped by what it does, not what kind of object it is. `internal/daemon/handlers_settings.go` owns the settings domain. `internal/engine/dep_graph.go` owns the dependency-graph concept. A file called `services.go` or `helpers.go` raises the question "what domain?" — name it for the answer.

### Composition over service indirection

Prefer calling a function directly over wrapping it in an injected service. Prefer a struct with a small, explicit API over a thin interface that exists only to name the layer. One useful counter-example: `Orchestrator` exposes methods like `UpdateDetachedDeps` and `GetServiceInfo` — that is not service indirection, it is encapsulation of mutable state behind a clear boundary.

### Naming carries intent — comments describe rationale, not action

If you need a comment to explain what a line of code does, the line is under-named. Rename the function, variable, or type until the code speaks for itself. Reserve comments for the things code cannot express on its own: the *why* behind a non-obvious choice, an external constraint, a deliberate trade-off.

---

## 2. When to write a comment

**The default is no comment.** Not "no WHAT-comments" — none. A WHY-comment
that merely restates what the code and its names already convey is still
noise, and most explanations belong in the name, the test, or the commit
message instead. Reach for those first:

- Explaining what a function does → name it better; add a test that shows it.
- Explaining why a change was made → the commit message, where it stays with
  the change and does not rot in the file.
- Explaining a contract or a rule → the doc that owns it, linked from a test
  name if it needs to be enforced.

Write a comment only when a future reader would otherwise reintroduce a bug —
an invisible constraint that no name or test can carry. The bar is high, and
the examples below meet it.

### Write a comment when:

**Explaining why a non-obvious choice was made.**
The tooltip singleton in `ui/src/lib/tooltip.svelte.ts` is the canonical example. The `owner === node` guard exists because Svelte's `update()` fires on every reactive change across *all* live `use:tooltip` elements in the same microtask — without the guard, an SSE tick stomps the hovered tooltip with whatever element updated last. The comment explains a bug that would otherwise be invisible from the code:

```ts
// The `owner` field is load-bearing. Svelte action `update()` fires on every
// reactive change to the bound props, and reactive store updates (e.g. SSE
// ticks) trigger update on *every* live `use:tooltip` on the page in the
// same microtask. Without `owner === node` gating, the hovered tip's text
// gets stomped on by whichever element happens to update last.
```

**Documenting an external or environmental constraint.**
`internal/daemon/config_staleness.go` explains why transient editor saves and atomic-rename windows are treated as unknown rather than stale:

```go
// fileStamp hashes a config file. ok=false when the file can't be read —
// callers treat that as "unknown", never as stale (transient editor saves
// and atomic-rename windows shouldn't flap the flag).
```

**Explaining a wire-format or API shape constraint.**
`daemon/wire_more.go` explains why `SettingsResponse` duplicates fields instead of embedding `APIResponse`:

```go
// SettingsResponse is the /api/settings response. Fields duplicate APIResponse
// rather than embed it so the wire shape stays flat for TS codegen.
```

**Documenting atomicity or concurrency guarantees.**
`atomicio/atomicio.go` explains the rename-is-atomic guarantee and its precondition:

```go
// writeFileAtomic writes data to path via a temp file + rename. The rename
// is atomic on POSIX when source and dest are on the same filesystem, which
// is guaranteed here because the temp file is created in the target's dir.
```

**Marking a deliberate trade-off.**
If you accepted O(N²) because N is bounded, say so. If you chose a simpler algorithm because profiling showed this path is cold, say so.

**Flagging a future revisit.**
TODOs are acceptable when they carry context: who, when, and why not now. A bare `// TODO` with no context is noise.

### Don't write a comment when:

- It describes what the code does step-by-step. Rename or extract instead.
- It duplicates the function name in prose form.
- It summarises a block whose name already says the same thing.
- It is a leftover from a previous design that no longer applies.

---

## 3. Refactoring signals — the WHAT-comment rule

Martin Fowler's *Refactoring* (2nd ed., "Comments" smell) and Robert C. Martin's *Clean Code* (Chapter 4) both reach the same conclusion: the need to write a comment that explains *what* the code does is a signal that the code needs better structure, not more explanation.

Fowler: *"When you feel the need to write a comment, first try to refactor the code so that any comment becomes superfluous."* The recommended tools are Extract Function and Change Function Declaration (rename).

Robert C. Martin identifies "noise comments" — comments that merely restate the code in prose — as actively harmful because they add length without information and go stale without warning.

**Patterns to catch in review:**

| You wrote this comment | The fix |
|---|---|
| `// computes detached deps for this service` | Delete — `detachedDepsFor(name)` already says that |
| `// loop over services to find the running ones` | Extract `runningServices(services)`, delete comment |
| `// build the list of enabled features` | The function is named `enabledFeatures(cfg)` — delete |
| `// check if node is owner before mutating` | Already in the guard condition — delete |
| Long comment explaining how a function works | Split the function until each piece is self-explanatory |
| Comment needed to understand a variable name | Rename the variable |

A comment that survives this filter is almost certainly a WHY comment and belongs.

---

## 4. Domain organisation — Go

**Files are named for the domain they own, not the layer.**

Good: `handlers_settings.go`, `handlers_graph.go`, `config_staleness.go`. Each name answers "what domain does this file own?". Bad: `services.go`, `helpers.go`, `utils.go` — these answer "what kind of thing is this?" which tells you nothing about what it does.

**A package answers "what does this domain do?"**

`internal/engine/` owns the dependency graph, orchestration, and scheduling. `internal/daemon/` owns the HTTP API, SSE, settings, and environment lifecycle. If a concept spans multiple packages ask which package *owns* it. `detachedSet` lives in `engine/dep_graph.go` because detach is a dependency-graph concept; it doesn't belong in the daemon package just because the daemon calls it.

**Cross-domain helpers go in the domain that owns the concept.**

Resist creating a `shared/` or `common/` package as a catch-all. If the concept has a home, put it there. If it genuinely has no natural owner, create a minimal package with a specific name (`internal/fsutil`, not `internal/utils`).

**File size.**

Aim for 200–400 lines. Files above ~800 lines are a signal that a domain has been merged with a neighbour; split along the boundary you can find.

---

## 5. Domain organisation — Svelte / TypeScript

**Component files mirror the user-visible feature.**

`NodeDrawer.svelte` is correct — a drawer is a UI concept, and the name says what it draws. `Components.svelte` or `Panel.svelte` would not be acceptable names because they describe a technical layer, not a feature.

**Sub-components live beside their parent when they serve only one parent.**

`NodeEnvPanel.svelte` and `NodeDepsPanel.svelte` live next to `NodeDrawer.svelte` under `components/graph/`. They are not reusable generic components; they exist to serve the drawer. Co-location makes this visible.

**Stores split by domain.**

`store.graph` holds graph dashboard view state (selection, preview, env-switch progress). `store.daemon` holds daemon live state populated from SSE. `store.ui` holds ephemeral UI state (toasts, modals). This split exists so state is found by purpose, not by scrolling a flat bag. See `ui/src/lib/stores.svelte.ts` for the canonical structure.

The split also has a correctness consequence: `resetForNewDaemon` clears only `store.daemon` on reconnect. `store.graph` is not cleared because wiping it causes a blank canvas flash; `store.ui` is not cleared because user intent (open modal, active SQL mode) must survive a daemon restart. This decision would be invisible without the domain boundary.

**Pure helpers belong in `lib/<domain>.ts`, not `lib/utils.ts`.**

`lib/graphActions.ts`, `lib/logClassify.ts`, `lib/traceColor.ts` — each name says what domain it owns. A `lib/utils.ts` file is a magnet for unrelated code and becomes difficult to navigate.

**Generic UI primitives** (components that could exist in any project) belong in the top-level `components/` directory: `Btn.svelte`, `Toast.svelte`, `ConfirmModal.svelte`. Feature-specific components belong under `components/<feature>/`.

---

## 6. Composition over service indirection

Prefer direct calls over injected intermediaries.

```go
// Prefer this:
deps := graph.DepsOf(name)

// Over this:
deps := svc.GetDependenciesForService(name) // where svc wraps graph
```

The second form exists only if the intermediary adds something (state management, caching, access control). If it's a pass-through, the layer costs more than it provides.

**Prefer a concrete struct with methods over a thin interface.**

Interfaces are useful at package boundaries where the caller and implementation live in different packages, or where you need to stub in tests. Inside a package, concrete types are clearer.

**The `Orchestrator` is not a service layer.** It exposes `UpdateDetachedDeps`, `GetServiceInfo`, `StartService`, etc. because it owns mutable state that must be accessed from multiple goroutines behind a single lock. That is encapsulation, not indirection. The distinction: a service layer wraps logic that could have been called directly; an orchestrator owns state that cannot be accessed directly.

---

## 7. Comment audit checklist

Run this against your own PR before requesting review:

- [ ] Every comment answers "why", not "what"
- [ ] No comment duplicates the function signature in prose
- [ ] No bare `// TODO` — each one has a name, date, and context
- [ ] No comment that is older than the code beneath it (check git blame if uncertain)
- [ ] If a comment is needed to navigate a function, the function is probably too long — split it

---

## 8. References

**Martin Fowler, *Refactoring: Improving the Design of Existing Code*, 2nd ed. (2018) — "Comments" smell (Chapter 3).**
Fowler argues that comments are often "used as a deodorant" for bad code. His primary recommendation: before writing a comment to explain a block, try Extract Function or Change Function Declaration (rename) to make the comment unnecessary. Comments that survive refactoring are legitimate; they explain why, not what.

**Robert C. Martin, *Clean Code: A Handbook of Agile Software Craftsmanship* (2008) — Chapter 4: Comments.**
Martin distinguishes good comments (explanation of intent, warning of consequences, amplification of non-obvious importance) from noise comments (restatements of what the code does, redundant Javadoc, journal comments). His core claim: every comment is a failure to express something in code, and the goal is to minimise that failure by improving the code rather than patching it with prose.

---

## 9. Error handling (Go)

See `.claude/rules/error-handling.md`.

Wrap errors with `%w`; classify via sentinel/typed errors and `errors.Is`
/ `errors.As`, not by string matching on `err.Error()`.

## 10. Mutable state (Go)

See `.claude/rules/go-mutability.md`.

Mutable shared state must be encapsulated in a receiver type that owns the
lock. Document lock preconditions in comments on any exported field
requiring a held lock.

## 11. Callbacks vs interfaces (Go)

See `.claude/rules/go-callbacks.md`.

For intra-package signalling, prefer function-field callbacks
(`OnOutput`, `OnExit`) over thin interfaces. Define interfaces only for
test mocking or cross-package boundaries.

## 12. Event-loop drop policy (Go)

See `.claude/rules/go-event-loop.md`.

Event loops with subscribers must document their drop policy in the
type's comment. Non-blocking sends drop slow subscribers (acceptable for
observational subscribers); control-plane subscribers need a separate
channel.

## 13. SSE vs poll (Svelte / TS)

See `.claude/rules/svelte-async.md` and `.claude/rules/svelte-error-surface.md`.

SSE for real-time multi-consumer state; polling for long-duration tasks.
Don't mix patterns on the same data source. Surface unrecoverable errors
via toast or panel state — never silently swallow.

## 14. Component-scoped types vs `lib/types.ts` (Svelte / TS)

See `.claude/rules/svelte-types.md`.

Define type aliases inside the `.svelte` script unless used by 3+
consumers; then extract to `lib/<domain>-types.ts` with a domain prefix.
Comment exported component-scoped types with their consumers.

## 15. Accessibility ground rules (Svelte)

See `.claude/rules/svelte-a11y.md`.

All interactive elements need visible text or `aria-label`. Modals use
`role="dialog"` + `aria-modal="true"` and either `aria-label` or
`aria-labelledby`. Status indicators use `role="status"`.

## 16. Loading state pattern (Svelte)

See `.claude/rules/svelte-loading.md`.

Use `$state` boolean flags (`loading`, `busy`) paired with `disabled` and
`aria-busy="true"`. Skip skeleton screens unless content height varies
meaningfully.

## 17. Domain entry points (Go)

See `.claude/rules/go-domain-model.md`.

Repeated multi-step setup across 3+ call sites is a signal the domain
hasn't exposed the right method. Push it down to the owning domain along
with its sentinel errors and user-facing hints. CLI-side `requireFoo()`
helpers are a smell.

## 18. Test at behavioral boundaries

See [testing.md](testing.md).

Orbit favors end-to-end journeys and sociable domain tests. Solitary tests are
for algorithms, parsers, escaping, security boundaries, concurrency invariants,
and stable wire contracts—not DTO builders, getters, thin wrappers, or private
call sequences. Prefer one test through a public behavior over several tests
for its implementation helpers.
