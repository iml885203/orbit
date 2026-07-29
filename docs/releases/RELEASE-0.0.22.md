# Orbit v0.0.22 — see the value before the control plane

This preview gives the first successful Orbit session one canonical next
action. The public quickstart now opens the working application before it
introduces the operational dashboard.

## What changes for users

### One first-value handoff

The English, Traditional Chinese, Unix, and Windows quickstarts now end with:

```bash
orbit open demo-api
```

That matches the action already returned by `orbit up` and `orbit status`.
Following the primary instructions lands on the demo's proof-of-value page,
not a second competing surface.

The page shows that Orbit started Python on the host, waited for Redis in
Docker, and injected their connection details. Refreshing it advances a visit
counter stored in Redis. Only after that visible result does the quickstart
introduce bare `orbit open` as the way to inspect and control the environment
in the dashboard.

### Stronger installed-user evidence

The first-five-minutes acceptance test now proves all of these as one journey:

- the primary `open` target is `service`, with the selected runtime URL;
- `up`, `status`, and both READMEs recommend the same command;
- dynamic port recovery still resolves the correct service URL;
- two requests advance the Redis-backed counter;
- the dashboard remains separately reachable as the secondary control plane.

## Verification

- public v0.0.21 new-user audit from an empty directory
- `scripts/test-first-five-minutes.sh` with both preferred ports occupied
- `make preflight`
- public GitHub CI and release platform smoke

This release remains pre-1.0 and does not change the stable-release approval
gate.
