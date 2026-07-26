---
name: orbit-review
description: Orbit-specific code review against the project's conventions in docs/CODE_CONVENTIONS.md and .claude/rules/. Fans out three parallel reviewer sub-agents (simple-design / cohesion / reuse) and aggregates findings by fingerprint. On-demand only. Triggers on `/orbit-review` or `/orbit-review <base>`.
---

# orbit:review

On-demand code review for orbit. Spawns three parallel reviewer sub-agents, deduplicates by fingerprint, renders a severity-tiered report.

## Triggers

- User invokes `/orbit-review` — review unstaged + staged working-tree diff.
- User invokes `/orbit-review <base>` — review `git diff <base>...HEAD` (e.g. `/orbit-review main`).

This skill is **on-demand only**. Do not auto-trigger from hooks or other skills.

## Execution

### Step 1. Collect diff

```bash
# /orbit-review (no args)
git diff HEAD                   # unstaged + staged combined

# /orbit-review <base>
git diff <base>...HEAD          # branch vs base merge-base
```

If the diff is empty, output `No changes to review.` and stop.

Capture the list of changed file paths (`git diff --name-only`).

### Step 2. Determine relevant rule files

For each changed path, match against rule frontmatter:

| Path pattern matches | Add these rules |
|---|---|
| `**/*.go` | `.claude/rules/error-handling.md`, `.claude/rules/go-*.md`, `.claude/rules/domain-organization.md` |
| `**/*.svelte` | `.claude/rules/svelte-*.md`, `.claude/rules/domain-organization.md` |
| `**/*.ts` | `.claude/rules/svelte-*.md`, `.claude/rules/domain-organization.md` |

Deduplicate the resulting list. Expand globs (`go-*.md`, `svelte-*.md`) using `ls`.

### Step 3. Fan out three reviewer Task calls in ONE message (parallel)

For each `LENS` in `[simple-design, cohesion, reuse]`:

1. Read `.claude/skills/orbit-review/reviewer-prompt.md`.
2. Substitute placeholders:
   - `{LENS}` → lens name
   - `{LENS_FILE}` → `.claude/skills/orbit-review/lenses/<LENS>.md`
   - `{RULE_FILES}` → newline-separated list from Step 2
   - `{SCOPE}` → `git diff HEAD` or `git diff <base>...HEAD`
   - `{DIFF}` → the full diff text
3. Dispatch via Task tool, `subagent_type: general-purpose`, all three in one message so they run in parallel.

**Model.** Run the three lenses on **Sonnet** (`model: "sonnet"`). They are
bounded, structured, read-heavy analysis passes: Sonnet reasons well enough to
catch lock-discipline, wire-shape, and cross-file duplication issues, runs
faster, costs less, and — importantly — does not drain the Opus credit pool,
which matters when fanning out three at once (an Opus fan-out here exhausts
credits mid-review). Haiku is too weak for this — it misses the subtle
cross-file findings these lenses exist to catch; reserve it for trivial
mechanical scans only. Escalate to Opus/Fable **only** for a final adversarial
verify of a single high-stakes finding, never for the parallel lens sweep.

### Step 4. Aggregate

Collect the three outputs. For each:
- If output is `NO FINDINGS`, skip.
- Otherwise, parse each line as JSON. Discard lines that don't parse.

Build a map keyed by `fingerprint`. For each finding:
- If fingerprint already seen, append the lens name to the existing entry, increment `confidence` by 1 (cap at 10), and set `multi_lens_confirmed: true`.
- Otherwise, insert as new.

### Step 5. Filter by confidence

- `confidence >= 7`: include in main report.
- `confidence 5–6`: include in main report with `(low-confidence)` tag.
- `confidence < 5`: include in appendix only.

### Step 6. Render report

```markdown
# Code Review Report

**Scope:** {SCOPE}
**Files reviewed:** {N}
**Findings:** 🔴 {critical} · 🟡 {should_fix} · 💡 {consider}

## 🔴 Critical

### {category} — {path}:{line}
**Summary:** {summary}
**Detected by:** {lenses, comma-separated} {🔁 if multi_lens_confirmed}

```{lang}
{current}
```

Suggested:

```{lang}
{suggested}
```

**Reason:** {reason}

---

## 🟡 Should Fix
...

## 💡 Consider
...

## Appendix (low-confidence findings)
...

## Verdict

- [ ] APPROVE — no Critical findings
- [ ] APPROVE WITH WARNINGS — Should-Fix findings present; acknowledge before merge
- [ ] BLOCK — Critical findings present
```

Choose verdict based on finding counts: any Critical → BLOCK; any Should-Fix → APPROVE WITH WARNINGS; else APPROVE.

### Step 7. End

Output the report to the chat. Do not commit anything. Do not push. Do not modify any file in the working tree.
