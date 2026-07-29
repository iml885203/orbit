# Orbit v0.0.26 — move from the demo to your project

The public demo now leads to a ten-minute, local-first adoption path. A user can
prove Orbit beside an existing project before learning shared environment
repositories or persistent settings.

## What changes for users

Put one `orbit.yaml` in the project root, then use the same five-command loop
for the whole trial:

```text
doctor → up → open → logs → down
```

This path needs no environment repository, no `orbit init`, and no manual
editing under `~/.orbit`. The guide draws the boundary between:

- project code and its local config;
- host processes and Docker containers coordinated by Orbit;
- Orbit-managed runtime state;
- the optional shared environment repository used only after the file is
  proven.

The public mini-shop and both READMEs now expose this next step directly, so
the experience no longer ends after the demo.

## Less command ceremony

Host-process commands now support quoted arguments and `$VAR` expansion from
the service environment. This makes the portable form users naturally write
work as expected:

```yaml
command: python3 -m http.server "$PORT"
```

If the preferred port is occupied, Orbit selects another port, injects it as
`PORT`, and passes the selected value to the process without requiring a config
edit. Orbit still executes the parsed process directly; shell operators require
an explicit shell.

## From local proof to team sharing

The local-first guide explains the exact promotion step:

```text
project/orbit.yaml             local learning and validation
team-env/envs/dev.yaml         stable intent distributed through Git
```

When the file moves, project-relative `path: .` becomes
`path: ${WORKSPACE_ROOT}`. `orbit init --env-repo ... --env dev` then asks for
the workspace only because the selected environment actually needs it.

## Verification

- Unit coverage for quotes, service-environment expansion, escaped variables,
  and malformed commands.
- A real empty-project acceptance journey with an empty `ORBIT_HOME`.
- Preferred Redis and application ports occupied throughout the journey.
- Real `doctor`, `up`, service `open`, HTTP request, retained `logs`, and
  `down` assertions.
- Static EN/ZH guide contract and demo CTA validation.
- Full `make preflight`, platform smoke, and release gates.

This remains a pre-1.0 preview and does not establish the `v1.0.0`
compatibility contract.
