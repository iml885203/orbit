# Orbit v0.0.20 — first use starts from anywhere

This preview removes two sources of onboarding complexity: the installed-user
quickstart no longer assumes an Orbit source checkout, and the source-checkout
showcase now has one beginner journey instead of multiple modes and dashboards.

## What changes for users

### Installed quickstart

The README path is now:

```bash
orbit init --yes
orbit up
orbit status
orbit open
```

It works from an arbitrary empty directory. CI installs a real binary, runs
these steps against the public demo, and calls the healthy application endpoint.

### Mini-shop showcase

- One primary action completes cart, payment, order, and shipment.
- Three visible checkpoints prove readiness, order creation, and shipment
  linkage.
- The payment-failure path appears after success and offers one recovery action.
- Manual controls and the eight-API dependency graph use progressive
  disclosure.
- One smoke script replaces overlapping compact, onboarding, advanced, and
  release-check scripts.
- Health checks no longer leave `customer-api` or `cart-api` stuck during a
  clean start.
- Declined-payment evidence now shows that the current attempt created neither
  an order nor a shipment, instead of retaining the previous success.
- Demo SQLite files no longer dirty the source checkout.

The backend value remains: a frontend, eight APIs, SQLite, and Redis demonstrate
host/container orchestration and real data relationships.

## Verification

- `make preflight`
- CI `first-five-minutes` from an empty directory
- Ten resources healthy in a real Orbit start
- `bash docs/examples/mini-shop/scripts/smoke.sh`: successful linked shipment
  and safe HTTP 402 decline
- Desktop browser: success and failure paths
- 390 px browser: no horizontal overflow; advanced controls remain collapsed

This release remains pre-1.0 and does not change the stable-release approval
gate.
