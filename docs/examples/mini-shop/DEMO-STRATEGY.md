# Mini-shop demo strategy

Mini-shop exists to make Orbit's value visible, not to maximize the number of
technologies in one repository.

## Product story

One browser action crosses host processes, a container, SQLite databases, and
eight dependent APIs. The result is useful only when the user can see the
relationship: a successful order has one linked shipment, while a declined
payment creates neither.

## Experience contract

- The first screen offers one primary action.
- Readiness, order creation, and shipment linkage are the only always-visible
  evidence.
- Failure simulation appears after the successful path establishes context.
- Manual controls and service topology are progressive disclosure.
- A service failure names the affected resource and one logs command.
- The demo uses Python's standard library and never installs dependencies.

## Scope decisions

The baseline keeps one language because package installation is not the value
being demonstrated. It keeps eight APIs because dependency ordering, health,
targeted logs, and relationship evidence become meaningful at that size.

Additional runtimes or observability services belong in a separate example.
They must not add modes, profiles, or setup choices to this first journey.

## Release evidence

Before publishing a preview that changes mini-shop:

```bash
orbit -c docs/examples/mini-shop/dev.yaml up
bash docs/examples/mini-shop/scripts/smoke.sh
orbit -c docs/examples/mini-shop/dev.yaml down
```

The browser journey must also be checked at desktop and narrow viewport widths.
