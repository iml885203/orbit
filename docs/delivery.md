# Verify and resume a delivery

[English](./delivery.md) · [繁體中文](./delivery.zh-TW.md)

Check which source and bytes were tested before merging or resuming a release.
The result is a local checkpoint of live GitHub evidence, not permission to skip
review, CI or the existing release approval.

```bash
python3 scripts/delivery_status.py --pr 123 --workspace /path/to/task --json
```

Use your allocated task workspace. After dispatching a release, add its exact
`--release-run` ID and `--tag` to the same command. Repeat those selectors when
resuming; the tool never guesses a publication from a nearby run. Use `--help`
for options. Human output summarizes progress; JSON retains individual jobs,
attempts, timings and candidate digests.

Artifact metadata reads use the exact selected ID, with at most three GETs.
Each request has a 10-second limit; retries wait 0.5 then 1 second, only for
404 or HTTP 500/502/503/504. A 404 can also hide an inaccessible resource; it is
not proof that metadata will become available. Explicit 401/403, rate limits,
malformed replies and identity/digest/expiry mismatches fail closed. Timeouts
fail without retry. These are bounded reads, never replacement uploads or
publication retries, and the same policy applies to the delivery observer.

![An approved candidate is built once, tested in parallel, then promoted unchanged](./assets/candidate-delivery.svg)

## What the workflow proves

The candidate producer builds one dashboard and six product executables. Its
manifest binds the source SHA, intended version, tools, inputs, run and producer
attempt to exact file digests. The uploaded artifact ID and archive digest are
paired with that manifest in a small identity artifact.

Native runners verify and execute those bytes. They retain source compilation
and OS tests, plus installer fixtures, uninstall and automatic-update scenarios.
All Unix targets exercise the .NET journey. SQL Server readiness uses the same
Linux candidate; its publish-engine tests still use a real SQL Server. Each
successful consumer records evidence bound to the artifact and binary digest.

Promotion requires all six native receipts and SQL evidence. It copies the
already tested inventory, then preserves checksum, SBOM, provenance and
immutable-release verification. It does not rebuild the dashboard or product
executables. The published eight-file inventory stays unchanged. Source-test
compilation and installer fixture builds are separate from candidate builds.

Relevant PRs run native checks early. Only known documentation paths may take
an explicit no-op; unknown inputs and an unsuccessful impact check cannot
bypass the native gate. The Linux .NET job is blocking. Publication checks the
latest canonical main CI run and actual job results, including the native gate,
rather than trusting a green aggregate that could hide a failed job.

PR artifacts are diagnostic. Only a separately approved, current-main release
workflow can promote its own candidate. PR jobs have read-only repository
permissions and no release signing authority. Repository branch-protection
settings are separate; this change enforces workflow and publication gates.

## Recover without repeating accepted actions

Run the observer after a state change, timeout or agent restart. It reads live
PR, issue, job, candidate, release and package-channel state before replacing
its checkpoint. Inspect failed or pending jobs before deciding the next step.
An inspection that exits successfully means evidence was read; it does not
mean every delivery gate passed.

A failed-job-only rerun may retain successful jobs from an earlier attempt of
the **same run and source**. The observer uses each job's newest result. A newer
failure or skip cannot be hidden by an older success. A changed PR head drops
old reported review evidence. Agent review notes remain labelled as reported,
separate from GitHub's review decision.

If publication already exists, verify that its immutable assets and commit
match the candidate. Resume unfinished verification or package updates. Never
create a replacement release or rebuild different bytes under that tag. The
promotion job reconciles an already accepted matching publication before a
retry, so a lost response does not trigger another create request.

API failures stop observation without turning the old checkpoint into fresh
success. Expired or missing candidate artifacts are incomplete evidence. A
newly built candidate needs its own checks; it cannot inherit another
candidate's receipts. The observer reads the small identity artifact rather
than downloading all executables on every inspection. Binary verification
remains the native and promotion jobs' responsibility.

The checkpoint stores selected evidence, not tokens, signed archive URLs or
private project configuration. Keep it in task storage and release that storage
when the task ends. Save any required long-term report before cleanup; Actions
candidate artifacts have a limited retention period.
