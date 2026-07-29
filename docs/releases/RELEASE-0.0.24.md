# Orbit v0.0.24

## Outcome

The default first run now demonstrates why Orbit is useful, not merely that it
can start a process. From an empty directory:

```bash
orbit init --yes
orbit up
orbit open demo-shop
```

The browser opens a small storefront backed by three host-side Python APIs,
three SQLite databases, and Redis in a container.

## What users can prove

- **Run checkout** creates an order linked to its inventory reservation and
  catalog product.
- **Try 99 items** returns a useful failure without committing an order or
  changing stock.
- Orbit starts the dependency graph in order and injects each dependency's
  actual runtime URL.
- All five preferred ports may be occupied; the complete application still
  starts on automatically selected ports.
- The demo requires Python 3 and Docker, but no package installation.

## UX impact

The first-run mental model is one short path:

1. initialize;
2. start;
3. open the application;
4. see a linked business result.

Users do not need to clone either repository, author YAML, learn the dashboard,
or manually reconcile ports before reaching value. The dashboard, logs, JSON
status, and the larger eight-API contributor example remain available as
follow-up paths.

## Verification

- Empty-directory `orbit init --yes` flow.
- Five-resource healthy-state assertion.
- Five simultaneous preferred-port collision recovery.
- Successful linked checkout.
- Insufficient-stock rollback invariant.
- Inventory compensation invariant.
- Public `orbit-demo` CI.
- `make preflight`.

This is a pre-1.0 preview release. It does not establish the `v1.0.0`
compatibility contract.
