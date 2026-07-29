# Orbit v0.0.23 — declare the dependency, receive its endpoint

This preview removes duplicated service URLs from portable multi-service
environments. A dependency edge can now carry the actual runtime endpoint into
the process that needs it.

## What changes for users

### Service-to-service wiring follows `depends_on`

When an upstream service declares `url`, every dependent host service receives
it as `<DEPENDENCY_NAME>_URL`. For example, `depends_on: [catalog-api]`
provides `CATALOG_API_URL`.

This keeps one source of truth:

- the upstream declares its canonical URL once;
- Orbit waits for that upstream to become healthy;
- automatic port recovery updates the runtime URL;
- downstream processes receive that selected URL;
- the dashboard attributes the injected value to its dependency.

An explicit environment variable still wins, so teams can intentionally route
a dependency elsewhere without Orbit silently replacing it.

### Ready for a portable multi-service showcase

The contract works for host processes whose preferred ports are both occupied.
A real binary E2E starts two Python services on automatically selected ports,
then proves the downstream service calls the upstream through the injected
runtime URL. This is the generic wiring needed to move the larger mini-shop
showcase into the public environment repository without hard-coded ports.

## Verification

- unit coverage for service URL injection, explicit overrides, and source
  attribution
- config + automatic-port integration coverage
- real binary E2E with two host services and occupied preferred ports
- `make preflight`
- public GitHub CI and release platform smoke

This release remains pre-1.0 and does not change the stable-release approval
gate.
