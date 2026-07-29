# Testing strategy

Orbit optimizes for confidence in user-visible behavior, not test count or
statement coverage. The default shape is a testing trophy:

1. A small set of end-to-end journeys proves the installed binary works with
   real Git repositories, files, processes, HTTP, and containers.
2. Most tests are sociable domain tests. They exercise a package through its
   public entry point with real collaborators that are fast and deterministic.
3. Solitary tests are reserved for dense algorithms, parsers, escaping,
   security redaction, concurrency invariants, and wire compatibility with
   many meaningful edge cases.

## What to test

Prefer one test that proves a complete behavior over several tests for the
private functions used to implement it.

Good sociable boundaries include:

- loading a real `orbit.yaml` from a temporary directory;
- syncing from a real temporary Git repository;
- calling a real HTTP handler with `httptest`;
- starting a real short-lived host process;
- running a Cobra command and decoding its CLI envelope;
- rendering and interacting with a Svelte component as a user would.

Use a fake only when the real collaborator is slow, nondeterministic,
privileged, destructive, or outside the process. Docker, the operating-system
browser, remote networks, clocks, and process signals are reasonable seams.
Do not mock one Orbit package merely to test another Orbit package's call
sequence.

Concurrency tests wait for an observable boundary such as a subscription,
callback, or state transition before taking the next action. Do not use a
fixed sleep to guess that a goroutine or component is ready. Keep retry
intervals in tests as short as the contract allows; production timing is
covered at the installed-user boundary.

## Tests not worth owning

Do not add tests whose only purpose is to:

- call an unexported builder and compare fields copied into a DTO;
- verify a getter, setter, constant, struct tag, or thin wrapper;
- assert the exact sequence of internal calls;
- cover every branch of presentation formatting independently;
- duplicate behavior already proved at a stronger boundary;
- preserve an implementation shape during refactoring.

Delete such a test when a sociable or end-to-end test proves the same contract.
Coverage may decrease when low-value execution is removed; that is acceptable.

## Required journeys

The release gate owns these installed-user behaviors:

- install, update, uninstall, and preservation of user data;
- `init → up → open → status/logs → down` from an empty directory;
- missing runtime and dependency recovery;
- local project adoption with one `orbit.yaml`;
- switching between project contexts;
- managed environment sync with reproducible repository provenance;
- startup failure, dependency blocking, and recovery;
- release artifacts on supported platforms.

## Cross-platform coverage

Linux CI owns the complete Go and UI suites. The platform matrix compiles the
full codebase, runs the packages with OS-specific implementations, exercises
the native installer lifecycle, and drives one clean-user binary smoke test.
Do not duplicate every database-neutral domain test across all six runners;
those tests cannot gain platform confidence from another operating system.

## Running tests

During implementation, run the narrowest sociable test that covers the changed
behavior. Run `make test` after a coherent feature group and
`make preflight` once before committing that group. Release-only journeys may
remain slower because they prove installed binaries and real platform behavior.

Use `make test-journeys` before a release candidate or after changing init,
environment selection, lifecycle orchestration, dependency recovery, or other
cross-process behavior. It builds once, then reuses that exact binary for the
empty-directory, local-adoption, and project-switch journeys.

When a test is flaky or slow, first ask whether it belongs at that level. Move
it to a stronger boundary if isolated mocks caused the fragility; move it out
of the inner loop only when the real dependency is inherently expensive.
