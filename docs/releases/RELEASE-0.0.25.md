# Orbit v0.0.25 — prove what did not change

The public mini-shop now separates a checkout attempt from the records that
already exist. A rejected action no longer hides a previous successful order.

## What changes for users

After **Run checkout**, the page measures:

```text
Available stock   8 → 7
New reservations  +1
New orders        +1
```

After **Try 99 items**, it measures again from the APIs:

```text
Available stock   7 → 7
New reservations  +0
New orders        +0
```

The durable-state section still shows the earlier Order and Reservation,
including their link. A first-time user can distinguish “this attempt added
nothing” from “the system contains nothing” without reading logs or trusting a
claim in the documentation.

Trying the failure first is equally explicit: stock remains `8 → 8`, both
record deltas are `+0`, and durable state correctly says there is no order yet.

## Why this matters

The demo's failure path was already transactionally correct, but its old UI
replaced Order and Reservation with `Not created`. That wording could imply the
previous checkout disappeared. The new presentation keeps two concepts
separate:

- **Last attempt:** what this action changed.
- **Durable state:** what currently exists.

This reduces the mental model while giving users stronger evidence.

## Verification

- Independent clean-user audit of the public v0.0.24 release.
- Browser success → failure journey.
- Browser failure-first journey.
- API-authoritative stock, reservation, and order snapshots.
- Smoke assertions that failure adds zero records and preserves the exact
  successful state.
- Public `orbit-demo` CI.
- Orbit empty-directory first-five-minutes acceptance.
- Full `make preflight` and release platform smoke.

This remains a pre-1.0 preview and does not establish the `v1.0.0`
compatibility contract.
