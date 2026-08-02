---
paths: ["**/*.md"]
---

# Documentation maintenance cost

**Rule**: Do not add documentation that must change whenever a feature
changes — command tables, code enumerations, capability matrices — unless
the tool cannot describe itself. When such a document is genuinely needed,
exactly one authoritative copy exists; every other location links to it
instead of mirroring it.

**Why**: Every mirror is a future drift. A table that tracks the CLI goes
stale the release after nobody remembers it exists; two languages double
the odds. Self-describing surfaces (`--help`, JSON envelopes, flag names)
never go stale.

**Good**: `agent-cli.zh-TW.md` says "the full error-code list lives in
[agent-cli.md](../docs/agent-cli.md#stable-error-codes)".

**Bad**: the same error-code table maintained in English and Chinese.

**Exception**: low-churn conceptual documents (architecture, philosophy,
adoption guides) may stay bilingual — they change when ideas change, not
when features do.
