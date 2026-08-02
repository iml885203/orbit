# Isolated runtime instances

[English](./instances.md) · [繁體中文](./instances.zh-TW.md)

Orbit's default runtime is intentionally backward compatible: it uses the
declared host ports, the default daemon socket and dashboard address, and the
legacy Docker resource names. Use a named instance when multiple checkouts,
agents, or CI jobs need to run independently on the same machine.

## Use a named instance

`--instance <name>` targets one isolated runtime and is available to lifecycle
and diagnostic commands:

```bash
orbit up --instance checkout-a --json
orbit status --instance checkout-a --json
orbit logs app --instance checkout-a --json
```

Keep the same instance target for the whole workflow. JSON responses include
the `instance` field, and their recommended actions retain that target.
Named dashboards show the instance in the header and browser tab. The default
runtime keeps the standard dashboard header without an instance badge.

List named instances to discover their environment, state, dashboard, and
resolved resource endpoints:

```bash
orbit instance list --json
```

## Isolation and ports

Each named instance owns a directory under `~/.orbit/instances/`, including its
daemon socket, log, state, ownership record, and resolved ports. It also owns a
Docker namespace, network, containers, and volumes.

The default runtime treats declared host ports as fixed. A named instance
treats them as preferences: Orbit selects available ports when necessary,
persists the result for stable restarts, injects the resolved ports into host
services, and reports the actual endpoints through `up`, `status`, and
`instance list`.

Callers should consume those reported endpoints instead of assuming the
declared port or coordinating `ORBIT_HOME`, `ORBIT_NAMESPACE`,
`ORBIT_DASHBOARD_PORT`, and `ORBIT_SOCKET` themselves.

## Cleanup

```bash
orbit instance clean checkout-a --json
```

Cleanup stops the instance and removes its daemon state and Docker resources.
It is destructive to that instance's local processes and data, but ownership
labels keep other named instances and the default runtime untouched. Run it
only when that instance is no longer needed.

Low-level runtime environment variables remain available for compatibility
and tests of Orbit's isolation internals. They are not the normal way to run a
second stack; see [Configuration](configuration.md#low-level-runtime-overrides).
