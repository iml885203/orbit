# Mini-shop

Mini-shop is Orbit's source-checkout showcase. It proves the product promise
with a stack large enough to need orchestration, while keeping the user journey
to one action.

## Start

From the Orbit repository root:

```bash
orbit -c docs/examples/mini-shop/dev.yaml up
orbit -c docs/examples/mini-shop/dev.yaml open
```

Open <http://127.0.0.1:3000> if the browser does not open automatically, then
choose **Run a successful checkout**.

The page shows three pieces of evidence:

1. all required resources are healthy;
2. inventory and payment complete before an order is committed;
3. the resulting shipment links to that same order.

After the successful path, **Then show a payment failure** demonstrates that a
declined payment creates neither an order nor a shipment. Manual controls and
the service graph stay collapsed until you ask for them.

Stop everything with:

```bash
orbit -c docs/examples/mini-shop/dev.yaml down
```

## What Orbit coordinates

The environment contains one Redis container and nine host processes:

| Resource | Responsibility | Local state |
|---|---|---|
| `web` | browser experience | none |
| `checkout-api` | checkout orchestration | none |
| `catalog-api` | product catalog | SQLite |
| `inventory-api` | stock reservation | SQLite + Redis |
| `customer-api` | customer records | SQLite |
| `cart-api` | customer carts | SQLite |
| `payment-api` | approved and declined payments | in-memory |
| `order-api` | confirmed orders | SQLite + Redis |
| `shipping-api` | tracking and order linkage | SQLite |
| `redis` | shared cache dependency | container volume |

The primary relationship is:

```text
web
  → checkout-api
      → cart-api → catalog-api
      → inventory-api
      → payment-api
      → order-api → catalog-api + customer-api + Redis
      → shipping-api
```

Every application uses Python's standard library. The example requires Python
and Docker, but no package installation.

## Repeatable verification

With the environment running:

```bash
bash docs/examples/mini-shop/scripts/smoke.sh
```

The smoke check proves:

- all eight backend health endpoints respond;
- one successful checkout returns exactly one order and one shipment;
- the shipment's `order_id` matches the created order;
- a declined payment returns the expected safe failure.

The script exits non-zero at the first broken invariant.

## Recovery

Start with Orbit's view of the environment rather than checking processes
individually:

```bash
orbit -c docs/examples/mini-shop/dev.yaml status
orbit -c docs/examples/mini-shop/dev.yaml logs checkout-api
```

The page also shows the first unhealthy API under **See the dependency graph**.
Once the affected resource is fixed, rerun only the primary checkout action;
healthy peers do not need a full restart.

## Scope

Mini-shop demonstrates Orbit, not an e-commerce architecture reference. It
intentionally keeps one runtime and standard-library dependencies so first use
is not dominated by package managers. Mixed-language environments belong in a
separate advanced example after this baseline remains fast and reliable.
