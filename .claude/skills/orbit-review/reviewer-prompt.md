# Reviewer prompt template

This template is fed to the Task tool (subagent_type=general-purpose). Placeholders are substituted by the orchestrator before dispatch.

Placeholders:
- `{LENS}` — lens name (e.g. `simple-design`)
- `{LENS_FILE}` — absolute or repo-relative path to `lenses/<LENS>.md`
- `{RULE_FILES}` — newline-separated list of `.claude/rules/*.md` paths relevant to the diff
- `{SCOPE}` — one-line description of what's being reviewed (e.g. "git diff main...HEAD" or "pull request #66")
- `{DIFF}` — the unified diff to review

---

## Prompt

You are reviewing a code change through the **{LENS}** lens for the orbit project.

Orbit is a Go (CLI + daemon) + Svelte 5 (UI) project. Its stated values:
- Simple design, prefer no service indirection
- Organise by domain, not layer
- Naming carries intent; comments explain WHY not WHAT
- Composition over thin interfaces

**Step 1.** Read the following rule files. They encode orbit's conventions:

{RULE_FILES}

**Step 2.** Read your lens-specific checklist:

{LENS_FILE}

**Step 3.** Review the diff below. Scope: {SCOPE}

```
{DIFF}
```

**Step 4.** Output your findings as one JSON object per line. Do not output prose, markdown, headers, or summaries. Each line must parse as a single JSON object with this schema:

```
{"severity":"CRITICAL"|"SHOULD_FIX"|"CONSIDER",
 "confidence":1-10,
 "path":"relative/path/to/file",
 "line":<number>,
 "category":"<short kebab-case category>",
 "summary":"<one-line description>",
 "current":"<exact problematic snippet, no surrounding context>",
 "suggested":"<exact corrected snippet>",
 "reason":"<one paragraph; cite rule file or CODE_CONVENTIONS §>",
 "fingerprint":"<path>:<line>:<category>",
 "lens":"{LENS}"}
```

If you find no issues, output exactly:

```
NO FINDINGS
```

Do not output anything else. No preamble, no closing summary, no markdown formatting.

## Severity rubric

- **CRITICAL**: explicit rule violation (CODE_CONVENTIONS §, named rule file), or introduces a bug / race / data-loss risk.
- **SHOULD_FIX**: strong smell against stated values but no explicit §-rule violation; non-trivial maintainability cost.
- **CONSIDER**: minor style or maintainability concern; optional.

## Confidence guidance

- 9–10: explicit rule violation with concrete evidence in the diff.
- 7–8: clear smell with high likelihood of being a real issue.
- 5–6: possible issue; reviewer should flag with caveat.
- 3–4: weak signal; likely false positive.
- 1–2: suppress.
