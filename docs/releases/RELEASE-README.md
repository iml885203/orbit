# Preview releases

GitHub Releases are the user-facing record of delivered Orbit changes.
Repository notes exist only for versions that were actually published or are
the single next release candidate. Files in this directory use the matching
`RELEASE-X.Y.Z.md` name; the GitHub release remains authoritative for what was
actually delivered.

Do not create speculative per-commit version notes. Accumulate a coherent
product slice in the next candidate, verify it, and get review before
publishing one release. A release should answer “what can the user do or
understand now that they could not before?”, not mirror the commit count.

Before tagging a preview, run:

```bash
make release-check RELEASE_VERSION=v0.0.39
```

The candidate notes and both bundled plugin manifests must match that version.
After the candidate commit passes CI and is reviewed, tag that exact `main`
commit and dispatch the Release workflow with `RELEASE v0.0.39`. The workflow
requires successful `preflight` and `first-five-minutes` checks from that exact
main commit, runs platform and SQL Server smoke gates, then publishes the
curated candidate notes as the GitHub Release body. It does not rerun the same
Linux source gate after those checks have passed. Commit-generated notes are
deliberately not used as the product delivery record.
