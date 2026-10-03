# ADR 0001: Ports and adapters with in-memory state

Status: accepted

## Context

The pool makes concurrency decisions (leases, queues, affinity, capacity) that are easy to get wrong
and hard to test against a live Foundry project. V2 may need a shared store for several replicas.

## Decision

Services depend on ports (`FoundryAdapter`, `SessionRegistry`, `AffinityStore`, `QueueManager`,
`MetricsRecorder`). V1 ships in-memory implementations of the stores and one Azure SDK adapter for
Foundry. Tests use a scriptable fake Foundry adapter.

## Consequences

- Pool and reconciler logic is tested deterministically, including races and cancellation.
- A shared store can replace the in-memory ones without changing the services. The port contracts
  (`try_lease`, compare-and-release by request id, slot reservations) were written with that in mind.
- The in-memory stores are only correct inside one process. See ADR 0003.
