---
name: orbit-feature-implement
description: "Implement an Orbit feature request or bug fix end to end from a required GitHub issue number or URL: validate that the issue is open, sufficiently clear, and independently verifiable; create an isolated git worktree; implement and test it; run Orbit code review with sub-agents; open a PR; resolve review and CI failures; merge and close the issue; update local main; and perform exploratory testing. Use when the user explicitly invokes `/orbit-feature-implement` or asks to implement an Orbit feature or fix an Orbit bug through PR and merge, including prompts such as `/orbit-feature-implement #13`, `implement issue 13 through PR and merge`, `fix bug issue 26 through verified merge`, or `實作這個 issue 並完成 PR 與合併`. Do not use for chores, refactors, investigations, open-ended discussions, requests without an issue identifier, or implementation requests that do not authorize the full PR-and-merge workflow."
---

# Orbit Issue Implement

Drive one well-defined Orbit feature request or bug fix from issue intake through post-merge exploratory verification. Keep the user informed at meaningful milestones and persist until the workflow finishes or reaches a genuine stop condition.

## Required input and intake gate

Require exactly one GitHub issue number or issue URL. If none is supplied, stop and ask for it. Do not infer an issue from branch names, recent history, or conversation context.

Resolve the issue with `gh issue view` and request machine-readable fields, including number, title, body, state, labels, and URL. Confirm all of the following before changing files or creating a worktree:

1. The issue exists and is open.
2. It requests a user-visible or developer-facing new capability or correction of incorrect behavior, rather than a refactor, maintenance task, investigation, or open-ended discussion. Treat labels as evidence, not the sole classification signal.
3. Its intended behavior, boundaries, and completion evidence are clear enough to derive concrete acceptance checks.
4. The requested behavior is compatible with the current repository, or any discrepancy is small enough to resolve without changing the issue's intent.

Write a short intake summary containing the issue goal, classification (feature or bug), explicit acceptance criteria, planned verification, and any assumptions. Map each acceptance criterion to observable evidence, and settle behavior-affecting command/contract choices before coding. Keep those decisions and scope boundaries in this one intake rather than rediscovering them during review. If classification or verifiability is materially ambiguous, stop before implementation and ask the user to clarify or update the issue. Do not silently invent product decisions.

## Prepare isolated work

1. Read `AGENTS.md`, `README.md`, `docs/CODE_CONVENTIONS.md`, and the relevant structured docs. Treat [the Definition of Done](../../../docs/CODE_CONVENTIONS.md#19-definition-of-done--pre-commit-checklist), including its documentation ownership and impact checks, as the single completion standard for this workflow. Read `DESIGN.md` for UI design changes and every applicable `.claude/rules/` file before editing matching Go, Svelte, or TypeScript files.
2. Inspect the current worktree without altering user changes. Fetch the remote and identify the repository's default branch.
3. Create a new worktree from the current remote default branch. Use a branch named `codex/issue-<number>-<short-slug>` and a narrowly scoped sibling worktree path. Never reuse or clean a dirty worktree.
4. Establish a baseline with the allocated workspace and the narrow isolated `go`, `ui` or `docs` mode in [development.md](../../../docs/development.md#isolated-checks). Parse its `--json` report; Go checks use its short socket alias rather than a raw test invocation under a long temporary path. Run the relevant existing tests. If the baseline fails, determine whether the failure is pre-existing. Stop and report it when it prevents reliable issue verification.
5. For a bug, reproduce the reported failure against the untouched baseline before designing the fix. If direct reproduction is impractical, record the concrete evidence for the failure and why reproduction is unavailable. Tie the regression test to the reproduced behavior or established root cause.

## Implement and verify

1. Trace the existing behavior and adjacent patterns before designing the change. Prefer the smallest cohesive implementation that satisfies the acceptance criteria.
2. Add behavioral tests that fail without the change and cover important boundary or error cases. For UI behavior, verify the user-visible interaction, not only helper functions.
3. Implement incrementally and run focused tests while working.
4. After changing any Go struct, config field, or tygo-emitted comment, run `make gen-types` and include generated TypeScript changes when applicable.
5. Run focused checks, then the independent review loop below. Reserve the fresh Completion gate for the stable reviewed source immediately before committing.

Do not weaken, skip, or delete meaningful tests merely to make the gate pass.

## Completion gate

Run this gate on the final reviewed source before every commit, including commits that resolve material review or CI fixes:

1. Walk the complete Definition of Done checklist; do not reproduce or shorten it in this skill.
2. Run `make preflight` fresh on the final source. Prefer `make check-full CHECK_WORKSPACE=PATH` with the task workspace allocated under the repository storage policy; it runs the same full gate in an isolated snapshot and retains fingerprinted logs. Narrow/reused checks do not authorize a commit. See [development.md](../../../docs/development.md#isolated-checks) for recovery and lint-baseline reductions. Run `make lint-new`, plus `make lint` when the touched area warrants the stricter lint pass.
3. Inspect the final diff for secrets, debug artifacts, accidental user changes, and anything outside the issue's authorized scope.

If the gate fails, fix the cause and run the whole gate again before committing.

## Review and commit loop

Spawn independent reviewer sub-agents in parallel against the implementation diff. Give each a distinct lens—simple design, cohesion/domain organization, and reuse/regression risk—and require them to read the applicable `.claude/rules/` files and `docs/CODE_CONVENTIONS.md`. Pass the raw diff and relevant source files, not the intended conclusions. Do not invoke `$orbit-review` from this skill because `orbit-review` is on-demand only and forbids automatic invocation by other skills. Treat the aggregated findings as an independent review gate:

- Resolve every Critical and Should Fix finding, or document concrete evidence that a finding is invalid.
- Re-run affected focused tests after changes; complete the independent re-review before the fresh Completion gate.
- Repeat the independent review after material changes until no blocking finding remains.

After the review findings and source edits stabilize, run the Completion gate fresh. Do not edit that source while its full validation is running; changed input invalidates its commit authority and requires another fresh gate. Focused checks can run during review.

Create a focused Conventional Commit that references the issue only after the Completion gate passes. Never use `--no-verify`.

## Open and review the pull request

1. Confirm the latest remote default branch is incorporated according to repository policy. If synchronization changes validated inputs, resolve conflicts, re-review as appropriate, and run the fresh Completion gate before any new commit. A fetch that changes no validated inputs does not require a duplicate full gate.
2. Push the issue branch without force and open a non-draft PR. Include the issue linkage (`Closes #<number>`), acceptance criteria, implementation summary, and exact verification performed.
3. Spawn at least one fresh sub-agent to review the actual PR diff and PR context. Do not give it the intended conclusions. Ask it to inspect correctness, regression risk, test coverage, and Orbit conventions.
4. Address every substantiated sub-agent finding. Re-run focused tests, obtain an independent pass when changes are material, then run the fresh Completion gate, commit and push.
5. Check GitHub review threads and requested changes. Resolve actionable feedback with code and tests; do not mark unresolved feedback as resolved merely to unblock merging.

## Delivery checkpoints

When measuring an issue, start the optional [phase journal](../../../docs/development-measurement.md)
at implementation intake and retain each span ID. Record independent reviewers
on separate tracks, explicit waits and rework reasons, and import validation /
delivery evidence after state changes. Resume the same journal after a restart;
never reconstruct earlier discussion or missing review times as observed work.
The resulting windows are not active coding time and do not replace any gate.

Use the repository-owned [delivery observer](../../../docs/delivery.md) after
state changes and before resuming after a timeout or restart. Supply exact
PR/run/tag selectors and the allocated task workspace. Read live job results,
including retained successful jobs from the same run when only failed jobs
were rerun. Treat missing, failed or unexpectedly skipped evidence as incomplete;
do not accept a green workflow aggregate alone. Agent review notes remain
reported evidence, separate from GitHub reviews.

The observer does not authorize mutations. Existing user authorization still
applies; before retrying an ambiguous merge or publication, reconcile the live
identity/state and perform only work that remains. A published immutable release
must be verified and its unfinished verification/package steps resumed, never
rebuilt or replaced. Release promotion uses the approved workflow's tested
candidate; a PR artifact cannot be promoted as release authority.

## CI and merge gate

Monitor the PR's required checks with structured CLI output where available. Do not merge while checks are pending, failing, skipped unexpectedly, or stale relative to the latest commit.

For each failure:

1. Inspect the failing job and logs.
2. Reproduce locally when practical.
3. Fix the root cause and run focused tests.
4. Re-run the review loop if the fix materially changes behavior or design, then run the fresh Completion gate, commit and push.
5. Wait for all required checks on the new head commit.

Merge only when all required CI checks pass, no blocking review remains, the PR is mergeable, and the acceptance criteria are still satisfied. Use the repository's normal merge method; never force push. Confirm that GitHub closed the linked issue after merge. If it did not, close it with a comment linking the merged PR.

## Post-merge local and exploratory verification

1. Return to the original local repository. Preserve unrelated local changes.
2. Fast-forward the local default branch from its remote. If local divergence or dirty files prevent a safe update, stop and report the exact condition instead of resetting or discarding anything.
3. Verify that the merge commit is present locally.
4. Exercise the implemented behavior from the user's perspective on the updated default branch. Cover the primary path, at least one meaningful boundary or failure path, and interaction with the closest existing behavior. Use Orbit's real CLI/UI/runtime where feasible; use `--json` whenever parsing Orbit CLI output.
5. Record the exploratory scenarios, observed outcomes, and any residual risk. If exploratory testing finds a regression, do not call the workflow complete: create or update an appropriate issue, and only make another code change when it remains within the user's authorized scope.
6. Remove the issue worktree only after the merge and post-merge verification succeed, and only when it contains no uncommitted changes. Delete the merged local issue branch when safe.

## Completion report

Report:

- issue and merged PR links;
- final branch and merge commit;
- acceptance criteria and how each was verified;
- automated tests, `make preflight`, code-review result, and CI result;
- exploratory test scenarios and outcomes;
- issue closure and local default-branch update status;
- any residual risks or intentionally deferred work.

Do not claim completion from a pushed branch, an open PR, or partially passing CI.
