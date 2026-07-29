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
make release-check RELEASE_VERSION=v0.0.41
```

The candidate notes and both bundled plugin manifests must match that version.
After the candidate commit passes CI and is reviewed:

1. Tag that exact Orbit `main` commit.
2. Update the demo to the same version, including its `ORBIT_VERSION`, then tag
   the exact demo `main` commit.
3. Wait for the demo's `validate` journey to pass. Before a GitHub Release
   exists, the journey builds the exact Orbit tag instead of a moving `main`.
4. Dispatch the Release workflow with `RELEASE v0.0.41`.

The workflow requires successful `preflight` and `first-five-minutes` checks
from the exact Orbit commit and the successful version-matched demo journey. It
then runs platform and SQL Server smoke gates and publishes the curated
candidate notes as the GitHub Release body. It does not rerun the same Linux
source gate after those checks have passed. Commit-generated notes are
deliberately not used as the product delivery record.
