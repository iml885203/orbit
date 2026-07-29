# Orbit v0.0.21 — the demo gets out of your way

This preview removes port troubleshooting from the first-use journey. Orbit
still treats real project ports as fixed contracts, while portable demos can
explicitly opt into automatic conflict recovery.

## What changes for users

### A quickstart that survives a busy laptop

The official demo prefers Redis on `26379` and its Python API on `28080`. If
either port is already occupied, setup now explains that Orbit will choose an
available port. `orbit up` does that automatically; users no longer need to
inspect a PID, stop unrelated software, or edit the synced YAML.

The selected runtime endpoint flows through the whole product:

- container bindings and host-process `PORT` injection
- dependency connection injection
- health checks
- dashboard and CLI status
- `orbit open demo-api`
- the `orbit.cli.v1` JSON contract

Normal project ports do not move. Environment authors opt in one port at a time
with an `ORBIT_AUTO_PORT_*` fallback expression.

### Stable across daemon restarts

Orbit recognizes a still-running managed container and preserves its selected
host port after a daemon restart. Host services are stopped cleanly and return
on the same selected runtime port when the environment starts again.

### Better acceptance evidence

The installed-user CI journey now deliberately occupies both preferred demo
ports before running the README flow from an empty directory. It succeeds only
when the API is healthy on dynamically selected ports. A real Docker E2E also
proves that restart keeps the container ID and both selected ports stable.

## Verification

- `make preflight`
- `scripts/test-first-five-minutes.sh` with `26379` and `28080` occupied
- daemon restart E2E with occupied container and host-service preferences
- runtime JSON ports and URL override static YAML preferences
- public `orbit-demo` CI

This release remains pre-1.0 and does not change the stable-release approval
gate.
