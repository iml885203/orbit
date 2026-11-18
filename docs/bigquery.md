# BigQuery development and integration tests

[English](./bigquery.md) · [繁體中文](./bigquery.zh-TW.md)

Orbit's compiled-in BigQuery extension manages opt-in development profiles and
isolated test datasets. It uses the official Go BigQuery REST SDK. It does not
provide a local BigQuery emulator, SQL proxy, query editor or migration engine.
SQL Server and BigQuery use the same internal extension seams; neither requires
installing a runtime feature plugin. The agent skill/plugin is a separate delivery
mechanism for instructions, not the database implementation.

## Enable one environment

Use the [complete example](https://github.com/iml885203/orbit/blob/main/docs/examples/bigquery/orbit.yaml). The top-level
`bigquery:` section is optional under environment schema `version: "3"`.
Existing environments do not need edits or GCP credentials. Environments using
this section require **Orbit v1.2.0 or later**; older binaries reject the unknown
section rather than silently ignoring it. Upgrade all developers/CI before
merging a BQ-enabled environment. Removing this section restores compatibility
with previous v3 binaries; retain recovery records and dispose of datasets first.
The YAML belongs in your environment repository's Git review process. The schema
version describes the shared format, not the feature inventory of every binary.

For `identity`, profile fields, schema/fixture formats and workspace path rules, see the [configuration reference](./configuration.md#bigquery).

Orbit discovers support from the **selected environment's configuration**.
Configured does not mean authenticated, reachable, billed, SQL-compatible or
application-ready. The dashboard, doctor and resources expose local intent and
recovery evidence; they never acquire ADC or make cloud requests. There is no BQ
node in the startup dependency graph. `orbit up` does not prepare a sandbox.

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

## Prepare, run the app, dispose

Run from your project, or pin the context with `--config` and `--instance`:

```sh
orbit --config docs/examples/bigquery/orbit.yaml bq inspect shared --json
orbit --config docs/examples/bigquery/orbit.yaml bq sandbox prepare integration --run pr-270-a --timeout 2m --json
orbit --config docs/examples/bigquery/orbit.yaml bq sandbox inspect integration --run pr-270-a --json
orbit --config docs/examples/bigquery/orbit.yaml bq sandbox list --json
```

`inspect shared` reads cloud dataset metadata and verifies location. It does not
run SQL or verify job creation. `sandbox inspect`/`list` read local receipts only;
check their observation time and `sandbox_data_ready`, not just exit success.
`prepare` creates a random, never-reused dataset ID, tables and load jobs using
`WRITE_EMPTY`. It records intent before effects, persists stable job IDs, polls
with a deadline, and distinguishes fatal job errors from warning diagnostics.
A pending/unknown result is not ready. Retry the **same profile and run key** to
reconcile the original operation. Changed profile/schema/fixture content conflicts
with that run; use a new key only after disposing of or explicitly accounting
for the earlier dataset. A cleaned run key cannot allocate another dataset.

The receipt's `app_config` maps your configured variable names to explicit
values, e.g. `ORDERS_DATASET=your-test-project.orbit_test_<random>`.
Orbit does not inject these values into running apps automatically. Read the
JSON envelope's `data.app_config` with a JSON parser, supply values through your
application's normal configuration mechanism, start the backend with its own
ADC, then exercise its actual public API and assert results. Do not `eval`
untrusted shell text. `app_runtime` remains `unverified`: seeding is not an E2E
pass. An example business assertion is totals A=300 and B=50 after the sample
fixture, obtained through your app's query/API, not an Orbit fake-query engine.

Install a `finally`/CI `always` teardown **before starting prepare**. Even if preparation or an assertion fails, stop any app you started, preserve canonical evidence, and attempt already-authorized cleanup only when its preconditions hold. Unknown jobs/generations retain resources for recovery; do not hide them with a new run key. SIGKILL or a disappearing runner cannot guarantee teardown.

Stop the application and wait for **all app/external jobs** to finish. Ensure
restricted writers and that no external actor can replace this dataset or reuse
its name, then explicitly confirm recursive cloud deletion:

```sh
orbit --config docs/examples/bigquery/orbit.yaml bq sandbox clean integration \
  --run pr-270-a --yes --app-stopped --restricted-writers --json
```

These flags are operational attestations, not locks or IAM enforcement. Orbit
checks the local canonical owner/runtime generation, immutable target, previously
observed dataset creation time/labels/location, and tracked submitted jobs before
deleting. Jobs that were durably never submitted are skipped; ambiguous
submission/job state blocks deletion. `down`, environment switches and
`instance clean` never delete BQ datasets implicitly. Table expiration defaults
to one day, but is not dataset expiration, cleanup completion or a billing cap.

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

For opt-in live validation, follow the prepare/app/dispose sequence above with a
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
