# Orbit v0.0.40 — declare each endpoint once

Orbit now uses a resource's declared ports as the source of truth for health,
opening applications, dashboard links, and service-to-service endpoints.

## What changed

- An HTTP or TCP health check can omit `port` when the resource declares one
  endpoint. HTTP checks prefer the `http` alias when a resource has additional
  ports.
- A service with an `http` or `https` port can omit `url`. `orbit status`,
  `orbit open`, the dashboard, and JSON output use the selected runtime port.
- Downstream services receive the inferred `<DEPENDENCY>_URL`, including after
  an automatic port remap.
- Ambiguous multi-port health checks fail validation and ask for one explicit
  port instead of probing port zero.
- The local-first example now declares each Redis and application endpoint
  once while retaining occupied-port recovery.

## Why it matters

The smallest useful `orbit.yaml` no longer repeats the same number across
`ports`, `health_check`, and `url`. Maintainers state the endpoint once; Orbit
keeps every consumer aligned with the actual runtime selection. This removes
configuration bookkeeping without hiding ambiguous intent.
