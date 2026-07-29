# Mini-shop UX smoke check

## Automated evidence

```bash
orbit -c docs/examples/mini-shop/dev.yaml up
bash docs/examples/mini-shop/scripts/smoke.sh
```

The script checks every backend health endpoint, a successful order/shipment
relationship, and a safe declined-payment path.

## Browser evidence

1. Open <http://127.0.0.1:3000>.
2. Confirm **Run a successful checkout** is the only primary action.
3. Run it and verify all three journey steps become complete.
4. Confirm the evidence cards show one order and a shipment with the same order
   ID.
5. Run **Then show a payment failure**.
6. Confirm the page calls the failure safe and offers one recovery action.
7. Expand manual controls and the dependency graph only after the primary path.
8. Repeat at a viewport narrower than 520px.

If services are unavailable, the page must lead to the first affected resource
and `orbit logs <resource>` rather than showing generic retry guidance.
