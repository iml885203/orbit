# BigQuery testing and recovery

[English](./bigquery-testing.md) · [繁體中文](./bigquery-testing.zh-TW.md)

Start with [BigQuery](./bigquery.md). Use this reference to configure permissions, plan tests, and recover interrupted operations or blocked cleanup.

## Credentials and permissions

Cloud calls run only in the invoking `orbit bq` CLI process with Application
Default Credentials (ADC), including a supported external-account/WIF ADC file.
Orbit never changes the global gcloud project, grants roles, copies credentials
into receipts or reuses the daemon's identity. Configure credentials outside YAML.
For local user ADC, an administrator may guide you through
`gcloud auth application-default login`; `gcloud auth login` alone is different.
For CI use your provider's WIF setup and a temporary ADC configuration file.

Shared inspection requires dataset metadata read permission. Sandbox preparation
requires dataset creation in `data_project`, table creation/data updates in the
new dataset, and `bigquery.jobs.create` in `job_project`. Recovery also needs job
metadata reads; cleanup needs dataset get/delete and table deletion permissions.
Set these deliberately for a development project using least privilege. The app
uses its own credentials and IAM; operator success does not prove app access.
No real GCP project was used to verify this implementation's offline tests.

## Data lifecycle and cleanup boundaries

Prepare uses a random, never-reused dataset ID, creates tables, and loads fixtures
with `WRITE_EMPTY` jobs. It persists intent and stable job IDs before side effects,
polls with a deadline, and distinguishes fatal errors from warnings. Changed
profile/schema/fixture hashes conflict with the same run; a cleaned key cannot
allocate another dataset. Account for the original resources before a new key.

Clean verifies the local owner/runtime generation, immutable target, previously
observed dataset creation time, labels, location, and submitted jobs. Only jobs
that were durably never submitted can be skipped; ambiguous submission or job
state blocks deletion. Teardown cannot be guaranteed after SIGKILL or runner loss,
so preserve recovery evidence.

## Recovery and manual disposition

Canonical records live under `<instance-base-home>/bigquery/<context-hash>/`,
outside disposable instance homes. Profile+run is the stable local lookup key;
configuration/input hashes are immutable attributes, not alternate lookup keys.
The local store UUID and OS locks coordinate processes sharing this canonical
directory. Independent computers/stores can create different datasets for the
same run key. They do not share a global registry or distributed lock.

An instance-home generation marker prevents a recreated same-name instance from
inheriting old mutation authority. `instance clean`, including missing-manifest
residue cleanup, retains the canonical records. List still shows old records;
a new instance cannot automatically mutate them. Do not copy the canonical
store to grant another runner ownership. Portable receipt copies are diagnostic
evidence only. Copying a whole store also copies its UUID; mutating clones is
unsupported. Archive records before uninstalling/purging Orbit's base home.

If automatic cleanup blocks because generation evidence is missing, a job cannot
be resolved, a target changed or an instance was removed:

1. Preserve the canonical JSON, job IDs and dataset full name. Do not edit a
   receipt to bypass checks or retry under another identity expecting takeover.
2. With an authorized cloud operator, inspect the exact project/location/dataset
   in GCP, reconcile the recorded jobs and any application jobs, and stop writers.
3. Resolve jobs to terminal state or cancel/wait using GCP's tools. Establish the
   intended dataset ownership; if uncertain, retain it and escalate to its owner.
4. Explicitly delete the verified dataset through GCP only after accepting the
   recursive deletion and replacement risk. Record the disposition in your test
   run/issue alongside the preserved receipt. Orbit's local record may remain
   `cleanup_intent`/not-ready; it is not proof of cloud state. Do not silently
   remove the evidence just to make the dashboard look clean.

BigQuery's documented dataset delete API has no generation-match precondition.
A GET followed by DELETE cannot fence an external replacement between calls.
Labels and creation time observations are evidence, not authorization or a
conditional delete. Safe operation requires restricted writers and non-reuse;
otherwise use manual disposition under an appropriate cloud control process.

## Testing strategy and opt-in cloud smoke

Offline CI uses the official SDK against a credential-free loopback HTTP fixture
server. It verifies request targets/location, lost replies/recovery, schema and
fixture handling, job diagnostics, locks (including another OS process), corrupt
records, independent stores, and cleanup refusals. It does not implement
GoogleSQL or prove ADC/WIF, IAM, quotas, billing or actual app behavior.

Unit tests should mock your backend's BQ boundary. Run its integration/E2E suite
against a real isolated dataset when a test project exists. An emulator can be
optional for narrow supported API scenarios; Orbit does not install one or count
emulator success as BigQuery compatibility. No GCP project is needed for the
unconfigured Orbit workflow or the offline CI gate.

For opt-in live validation, follow the [prepare/app/dispose sequence](./bigquery.md#prepare-run-the-app-dispose) with a
dedicated project, explicit location, appropriate ADC and app credentials. Start
with shared metadata inspection, then fixture preparation, then an actual
GoogleSQL/app assertion, and finally verified disposal. Without these prerequisites,
report **SKIP — no real GCP test project/credentials**. Never label offline tests
as this live smoke. Live cloud validation is a documented operator test, not a
silent default CI step.

References: [ADC](https://cloud.google.com/docs/authentication/application-default-credentials),
[Workload Identity Federation](https://cloud.google.com/iam/docs/workload-identity-federation),
[dataset delete](https://cloud.google.com/bigquery/docs/reference/rest/v2/datasets/delete),
[job status](https://cloud.google.com/bigquery/docs/reference/rest/v2/Job#JobStatus),
[managing costs](https://cloud.google.com/bigquery/docs/best-practices-costs).

Read-only live smoke entrypoint: set `ORBIT_BQ_LIVE=1`, `ORBIT_BQ_LIVE_CONFIG` (explicit YAML path), and `ORBIT_BQ_LIVE_PROFILE` (a shared profile), then run `go test ./internal/bigquery -run TestLiveSharedMetadata -v`. This verifies metadata only and does not replace the full app journey.

Observed table generations are persisted. A missing, expired or replaced table is never recreated/reseeded under the same run. If a job with durable submission intent returns 404, Orbit does not resubmit it: metadata retention and ambiguous submission require preserved evidence/manual disposition. Only a durably never-submitted planned job can be submitted for the first time.
