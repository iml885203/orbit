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
- Preserve secret and parameter references. Never copy a resolved secret into
  `orbit.yaml`.

Classify every resource as `supported`, `needs user input`, or `unsupported`.
Call out semantics Orbit cannot currently reproduce, including dynamic
resource expressions, generated connection strings or credentials, Aspire
service discovery, custom hosting integrations, callbacks evaluated at
runtime, and resources without an equivalent Orbit lifecycle or readiness
model.

Show the user:

1. the resources and dependency graph Orbit would own;
2. the project files and commands that would change;
3. prerequisites that remain the project's responsibility;
4. unsupported behavior and its impact;
5. the verification and rollback plan.

Get approval before creating or changing `orbit.yaml`. Removing the AppHost,
Aspire packages, or existing developer workflow is a separate destructive
migration step and requires separate approval.

## Implement and verify

After approval, create the narrowest project-local `orbit.yaml`. Validate it
before starting resources, then use the normal Orbit inspect/action/status
loop. Verify each Orbit-owned resource directly, including one real
application endpoint; wrapper-process readiness is not sufficient.

Migration is complete only when the requested development workflow runs
without Aspire. If an essential resource is unsupported, stop with the exact
gap and keep the project unchanged. Offer an Aspire wrapper only when the user
explicitly chooses coexistence or incremental adoption, and label it as a
wrapper rather than a migration.
