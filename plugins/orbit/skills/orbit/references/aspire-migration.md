# Migrating an Aspire AppHost

Use this workflow when a real project has an Aspire AppHost but no
`orbit.yaml`. Migration means making Orbit directly own the supported local
resources. Do not silently wrap `aspire run` as one opaque service and call
that a migration.

## Detect and inspect

Treat any of these as evidence of Aspire:

- `aspire.config.json` names an AppHost.
- A project uses `Aspire.AppHost.Sdk`.
- AppHost source calls `DistributedApplication.CreateBuilder`.
- A file-based or TypeScript AppHost is configured under `.aspire`.

Inspect the repository read-only first. Identify the AppHost, runtime and
container prerequisites, and existing developer workflow. Do not install
tools, generate artifacts inside the repository, or edit project files before
the user approves those actions.

If the Aspire CLI is available, evaluate the executable application model into
a temporary file outside the repository instead of attempting to parse
arbitrary AppHost source:

```bash
aspire do publish-manifest --apphost <apphost> \
  --output-path <temporary-manifest.json> --non-interactive --nologo
```

This command may restore and build the AppHost. Explain that scope and get
approval first. Treat the manifest as sensitive: it can contain parameter
names, resource expressions, generated credentials, and telemetry headers.
Read it locally, filter reports to the minimum structural fields, never paste
the raw manifest into chat or logs, and delete the temporary artifact after
the plan is complete.

## Plan the mapping

Build a proposed mapping before writing `orbit.yaml`:

- Map literal container resources to `containers` only when image, command,
  ports, volumes, environment, and health semantics are representable.
- Map project resources to `services` with their real project paths, startup
  commands, fixed endpoints when required, and readiness checks.
- Map executable resources to `services` only when their working directory,
  command, arguments, and environment resolve without Aspire at runtime.
- Convert wait relationships to `depends_on` only when the referenced Orbit
  resource and its readiness condition are explicit.
- Preserve existing secret and parameter references. Never copy a resolved
  personal, shared, staging, production, or externally authorized credential
  into `orbit.yaml`. A migration may instead define a new literal development
  credential when it is used only by an isolated local resource that Orbit
  creates, all consumers use that same local value, and the value grants no
  access outside that environment. Treat it as public repository data and do
  not reuse it for another environment.

Do not classify an Aspire expression as unsupported merely because it is
dynamic in the AppHost. First reduce it to the application contract and try
Orbit's existing runtime behavior:

- A service in `depends_on` receives `<DEPENDENCY_NAME>_URL` when the provider
  declares `url` or an `http`/`https` port. Hyphens become underscores and the
  key is uppercased. Explicit service `env` wins when the application expects
  another value.
- Redis, Kafka, MongoDB, and PostgreSQL dependencies receive Orbit's
  conventional host/port and connection variables. Inspect the application
  configuration keys and use explicit non-secret aliases when its names
  differ. Keep existing or externally authorized credentials as `${VAR}`
  references; a newly chosen local-only development credential may remain a
  literal when it satisfies the isolation boundary above.
- Named instances relocate ports before Orbit builds service environments, so
  prefer the injected dependency URL/connection values. For an initial direct
  migration, fixed declared ports are acceptable when the application needs a
  callback URL or configuration key Orbit cannot derive; record the resulting
  named-instance limitation instead of declaring the whole stack unsupported.
- If HTTPS fails only because the local development certificate is untrusted,
  inspect whether the project has an existing HTTP development mode. Prefer
  that documented switch for the evaluation; do not disable transport security
  in source or generate/trust certificates without approval.
- Exclude optional deployment branches such as Azure resources from a local
  run when the AppHost already makes them conditional. Do not treat an inactive
  deployment resource as a blocker for the requested local workflow.
- Inspect solution-wide build-output settings before mapping several projects
  as `type: dotnet`. If projects share a central artifacts directory, Orbit's
  independent builds may race on the same outputs. Build the repository once
  with its documented solution/filter command (serialize MSBuild when needed),
  then run the resulting applications directly under Orbit. Treat that build
  as an explicit project prerequisite and report it; do not misclassify an
  output-file lock as a runtime orchestration gap.
- When running prebuilt applications, preserve each project's working directory
  so content roots, appsettings files, and relative assets resolve as they do in
  the original developer workflow. A successful compile does not prove the
  process can be launched from the repository root.

Classify every resource as `supported`, `needs user input`, or `unsupported`
only after this mapping attempt. Unsupported means a required local behavior
cannot be expressed or safely approximated with current Orbit configuration,
not merely that Aspire represents it differently. Keep custom hosting
integrations, runtime-generated credentials, generated proxies, and callback
expressions visible in the plan, then prove whether each is essential by
starting the direct resources and exercising the requested workflow.

Show the user:

1. the resources and dependency graph Orbit would own;
2. the project files and commands that would change;
3. prerequisites that remain the project's responsibility;
4. unsupported behavior and its impact;
5. the verification and rollback plan.

An explicit request to set up or run the project with Orbit authorizes creating
or changing the narrowest project-local `orbit.yaml`; an assessment-only
request does not. Removing the AppHost, Aspire packages, or the existing
developer workflow is a separate destructive migration step and requires
separate approval.

## Implement and verify

After that scope is established, create the narrowest project-local
`orbit.yaml`. The first
attempt should use fixed, non-conflicting ports and the project's existing
local HTTP mode when available; this separates application wiring failures
from named-instance and certificate concerns. Validate before starting, then
use the normal Orbit inspect/action/status loop.

Start the infrastructure and services directly under Orbit; never invoke
`aspire run` during the migration verification. Recover one failure at a time
from Orbit's structured status and logs. Verify every Orbit-owned resource,
one real application endpoint, and the smallest representative user flow that
crosses service boundaries. Record the exact configuration or runtime behavior
behind any remaining failure; do not infer a product gap from the manifest
alone.

A failed first attempt is recovery evidence, not a reason to hand environment
ownership back to the user. Continue the inspect/log/fix/verify loop while the
next action is non-destructive and within the approved project scope. Stop only
for missing user intent or approval, an unavailable external prerequisite, or
a concrete required behavior that current Orbit configuration cannot express.
When stopping, report the executed evidence and the narrow gap instead of a
general statement that the project is too complex or Aspire-specific.

Match readiness to the protocol the application actually serves. If Orbit's
HTTP probe fails but a direct protocol-correct probe succeeds (for example an
HTTP/2-only development endpoint), record the mismatch and use the strongest
readiness check Orbit currently supports; do not report the application as
broken solely from the incompatible probe.

Keep the proposed environment portable. If a container bind mount only works
after replacing a repository-relative source with an absolute checkout path,
use that only as evaluation evidence and report the missing path-resolution
behavior; do not present the machine-specific YAML as a team-ready migration.

Migration is complete only when the requested development workflow runs
without Aspire. If an essential resource is unsupported, stop with the exact
gap and keep the project unchanged. Offer an Aspire wrapper only when the user
explicitly chooses coexistence or incremental adoption, and label it as a
wrapper rather than a migration.
