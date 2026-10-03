# ADR 0006: Per-agent circuit breaker

Status: accepted

## Context

When Foundry is down or very slow, every request waits for a timeout and the pool keeps sending
calls (and retries) to a service that cannot answer. That wastes caller time, ties up leases and
adds load to an already failing dependency.

## Decision

- One breaker per agent, because agent endpoints fail independently. It wraps the Foundry adapter
  (`CircuitBreakingAdapter`), so the request path, session creation and the reconciler all use it.
- States: closed, open, half open. `failure_threshold` consecutive failures open it. After
  `open_seconds`, `half_open_max_calls` probes are allowed. A successful probe closes it and a failed
  probe reopens it.
- **Only `FoundryUnavailable` and `FoundryTimeout` count as failures.** A 404, a rejected request, a
  failed session or a 429 shows that Foundry is answering. Throttling is already handled by bounded
  retries with `Retry-After`.
- A call cancelled by its caller counts as neither a success nor a failure and gives its probe place
  back. A failure after a stream has started counts as a failure.
- An open circuit raises `FoundryCircuitOpen`. The pool maps it to `503 FOUNDRY_CIRCUIT_OPEN` with
  `Retry-After` and never retries it.
- Defaults are on, 5 failures, 30 seconds, 1 probe. The agent list call is not guarded.
- Metrics: `pool_circuit_state` and `pool_circuit_transitions_total`. State is also in the admin
  agent view.

## Consequences

- Callers get a fast, explicit 503 during an outage instead of a timeout.
- During an outage a reconcile is `incomplete`, so nothing is deleted on the strength of a partial
  view.
- State is per process, is lost on restart and has no manual reset.
- Five failures in a row on a low-traffic agent can open the circuit on a brief blip. The effect is
  30 seconds of 503 for that agent. Tune per agent if that matters.
