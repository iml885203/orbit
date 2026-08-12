---
name: orbit-issue-create
description: "Draft an Orbit GitHub issue, verify every factual claim in it against the current code with independent reviewer sub-agents, and only file it once the claims hold and the request itself is sound. Use when the user asks to open, file, raise, or write up an Orbit issue, feature request, or bug report — including prompts such as `開 issue`, `幫我開 feature request`, `file a bug for this`, `raise an issue for the dacpac request`, `寫一張 issue`, or when a downstream consumer reports something that should become an issue. Also use before re-filing or substantially rewriting an existing open issue. Do not use for implementing an issue that already exists (that is `/orbit-feature-implement`), for reviewing code changes (`/orbit-review`), or for plain questions about the codebase."
---

# Orbit Issue Create

Write an issue whose every factual claim is verified against the code that exists right now, and whose request is worth making. Then file it.

The failure this exists to prevent is not a badly formatted issue. It is a *confidently wrong* one: an issue that reads well, cites real file paths, and sends whoever picks it up down a design that does not survive contact with the code. That issue costs more than no issue at all, because it converts one person's unverified reasoning into someone else's implementation plan.

## Why this needs a process

Issues written from a mental model of the code, rather than from the code, fail in a small number of recurring ways. Every one of these has actually happened in this repo:

- **The feature already shipped.** Written against a stale checkout, without fetching first. Whole scopes were requested that existed in the previous release.
- **"Only one place needs to change."** A `grep` found one central function, and that got reported as one seam — while three other call sites in other files made the same assumption. The issue's central claim was its weakest.
- **A cache or fingerprint rule that looks equivalent but is not.** Hashing a leaf artifact instead of its reference closure. The existing code had a comment explaining exactly this hazard, directly above the function that was read but not understood.
- **The issue contradicts itself.** The rationale said paths may be absolute; the scope said validation enforces relative containment. Both were written by the same author minutes apart, and neither was re-read.
- **The evidence was never re-run.** A probe result from an earlier session was cited as "verified", against a tree that had since moved.

The common root is always the same: **reasoning about how the code probably works, in place of reading how it does.** These are not caught by proofreading. They are caught by making someone check each claim against the source, and by re-reading your own draft as a whole.

## Intake

Establish these before drafting. Ask the user only what you genuinely cannot determine yourself.

1. **What is the observed problem or desired capability**, in the requester's own terms. If it came from a downstream consumer, keep their words — you will need them later to check that your framing did not drift.
2. **Which version was it observed on?** This is the single highest-value question for a bug report. A defect observed on an older release may already be fixed, and filing it as live is the most common failure above. Run `git fetch` and compare against the current default branch before believing any report.
3. **Feature or bug?** A bug needs a reproduction or an explicit statement that reproduction was not possible and why. A feature needs a boundary — what it is *not*.
4. **Who decides the open questions?** Anything that is a product or ownership call belongs to the user or the requester, not to you. Record it as an open question rather than quietly picking an answer.

## Draft

Write the body before verifying it, so there is something concrete to attack. Aim for prose that states what is true and where, not for a template to fill in.

A useful issue tends to carry:

- **Today / Expected** — the current behavior with file:line evidence, and what should happen instead.
- **Why it was not caught** — for a bug, the reason existing tests or gates missed it. This is often more valuable than the fix, because it usually names a coverage gap worth closing in the same change.
- **Scope** — what to change, in terms of the code that exists.
- **Not in scope** — the boundary. Also the place to record deliberate exclusions and *why*, so nobody re-litigates them.
- **Open questions** — decisions that are not yours.

Keep it honest about provenance. If something was established by reading rather than executing, say so in the issue itself and say what would confirm it. An issue that admits "verified by reading, not by running; one manual publish would confirm or refute this" is far more useful than one that implies more certainty than it has.

## Verify before filing

This is the part that catches the failures listed above, and it is not optional. Run it on the draft, before the issue exists.

### Self-check first

Cheap, and catches a class the reviewers often miss because they read sections in isolation:

1. **Re-read the whole draft as one document.** Every contradiction in the list above was between two sections that were individually fine. Check specifically that the rationale, the scope, and any example config agree on the same contract.
2. **Re-run any evidence you are citing.** Probes from earlier in the session, or from another branch, do not count. `git fetch` and confirm the claim still holds on the current default branch.
3. **Check the feature does not already exist.** Search for the field, flag, or behavior you are requesting. This takes one grep and has prevented an entire wasted issue more than once.
4. **Scan for anything that should not be public.** Downstream consumers' company names, internal project or repo names, env-file names, real schema object names, registry URLs, credentials. Neutralize them — the *structure* of a real example is what makes it useful, not the identifiers. If a real artifact is needed for verification later, arrange that privately rather than putting a path in the issue.

### Then independent reviewers

Spawn reviewer sub-agents in parallel, each with a distinct lens. Give them the draft and the repo — **never your conclusions**, and never a summary of what you think the answer is. A reviewer handed the intended verdict will confirm it; that is how a review becomes a rubber stamp.

Require each to read `docs/CODE_CONVENTIONS.md` and the applicable `.claude/rules/` files, and to cite `file:line` for every finding.

Useful lenses, adapted to the issue at hand:

- **Claim verification.** Take every factual assertion in the draft and check it against the source. Does that file:line say what the issue claims? Does that function behave as described? Is the "only one place" claim true — grep for the pattern and find every site. This lens catches the most and should always be included.
- **Boundary completeness.** What *else* touches this? For any claim that a change is contained, look for the adjacent assumptions: cache keys, fingerprints, state records, wire types, generated types, doctor checks, `--json` output, and both locale docs. This is the lens that finds the "one seam" error.
- **Request soundness.** Is this worth doing at all, and is the requested shape right? Would it violate a stated convention, add a guard for an unreachable condition, invert a dependency direction, or hard-code something the project deliberately leaves open? A reviewer briefed to argue *for* the change is often the one that kills it most convincingly.

Treat the aggregate as a gate. Resolve every finding, or record concrete evidence that it is invalid — and when a reviewer is right, say so plainly in the issue rather than quietly editing around it. An issue that notes "an earlier draft claimed X; that was wrong because Y" stops the next person from re-deriving the same mistake.

### When the review kills the issue

Sometimes verification shows there is nothing to file: the behavior already works, the report described an older version, or the request would make things worse. **Not filing is a successful outcome.** Report the finding to the user and to whoever raised it, with the evidence. Filing anyway — to have something to show — puts a permanently unactionable ticket in the tracker that someone will spend an afternoon re-deriving before closing it.

## File it

Only after the gate passes:

- Confirm the final body with the user when you made any judgment call they have not seen — especially a scope exclusion, or anything you decided differently from what a requester asked for. Say which calls were yours. A requester who asked for "a URL or a local path" and gets an issue scoped to local paths only deserves to know that was a decision, not an omission.
- File with `gh issue create`, with an appropriate label.
- Report the URL, and tell the requester what you decided on their behalf.

If the issue supersedes or corrects an existing one, link them explicitly in both directions.

## After filing

An issue is a claim about the future, so it decays. When new evidence arrives — a reviewer's finding, a downstream test result, a version confirmation — update the issue rather than leaving the original text to mislead. Note what changed and why, in the issue itself.

If a scope turns out to be already implemented or unreachable, withdraw it explicitly with the evidence. Silent deletion loses the reason.
