# Orbit v0.0.30 — recover the cause, trust the outcome

Orbit now turns a blocked dependency into one direct recovery action, while
the public mini-shop keeps each checkout attempt truthful through failure and
recovery.

## What changed

- A degraded Dashboard drawer follows `blockedBy` chains to the root resource
  and presents `Start <dependency>` as its primary action.
- Ineffective Restart and Stop controls are hidden from dependency-blocked
  graph nodes. Logs and inspection remain available.
- Starting the root dependency restores still-running dependents without
  restarting them.
- Process crash summaries use stderr from the exact process generation that
  exited. Ordinary stdout traffic and logs from an earlier run cannot become
  failure evidence.
- A signal-only exit such as SIGKILL reports the exact lifecycle reason
  (`exited: signal: killed`) without appending an unrelated successful health
  request.

## Public demo

- Every checkout click now replaces the previous attempt with an explicit
  pending, committed, rejected, or unavailable result.
- An unavailable attempt shows the requested quantity and time but does not
  claim stock, reservation, or order deltas that could not be measured.
- The last confirmed order remains under **Durable state**, separate from the
  failed attempt.
- Readiness changes to **A dependency needs attention** during the failure and
  returns to **5 resources ready** after recovery and the next successful
  checkout.
- A zero-dependency Node regression covers success → dependency unavailable →
  recovery in about 0.2 seconds.

The demo delivery is available in
[`iml885203/orbit-demo@6e476e7`](https://github.com/iml885203/orbit-demo/commit/6e476e7).

## Verification

- Direct and multi-hop Dashboard blocker tests resolve to the root dependency.
- A real Dashboard journey stopped inventory, opened the degraded frontend,
  started inventory from the frontend drawer, and observed the full graph
  recover to healthy.
- A real SIGKILL journey verified that JSON keeps the signal reason and omits
  the previous successful health request from `failure_evidence`.
- A real browser journey verified that failed checkout evidence replaces stale
  success while preserving the durable order, then returns to ready after
  recovery.
