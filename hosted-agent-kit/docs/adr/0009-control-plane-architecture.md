# ADR 0009: Kubernetes control-plane patterns inside one process

Status: accepted

## Context

The service already behaved like a small controller system: desired configuration, a session
registry, a scheduler, a reconciler, queues and health reporting. But the responsibilities were
tangled. Request handling created and deleted sessions and carried the cleanup, so a timeout or a
cancellation at the wrong moment could leave a remote session nobody tracked. One large reconciler
did observation, deletion, retries, warm capacity and version rollout. Status was a handful of
counters. A configuration change needed a restart and could not be seen as applied or not.

## Decision

Adopt the Kubernetes control-plane patterns that fit, in one process, without Kubernetes or a generic
API server.

- **Resources with spec and status.** `AgentPool` and `AgentSession` have a key, a `generation`
  (changes with the desired state), a `resource_version` (changes on every update), `finalizers` and
  `conditions`. `observed_generation` shows whether a configuration change has been applied. The
  session key is `(agent name, session id)`, because a session id is unique only within one agent.
- **Finalizers for cleanup.** A session is removed only when no finalizer is left. The cleanup
  finalizer holds it until Foundry confirms the remote delete. This replaces cleanup logic that was
  spread over request and reconciliation code, and it covers a failed delete, a cancelled creation
  and a creation that timed out.
- **Record at creation.** A session is stored the instant Foundry has created it, before it is
  ready. From then on something owns it. Creation failures mark it for deletion instead of deleting
  it inline and hoping.
- **Controllers.** Narrow reconcilers, each idempotent: `session` (finalizers), `observation`
  (list, compare, adopt or mark), `pool` (warm sessions, version drain), `gc` (stragglers),
  `health` (Foundry reachability), `status` (counts and conditions). A `ControllerManager` runs
  them with keyed, deduplicating, rate-limited work queues, per-key mutual exclusion and a graceful
  stop.
- **Watch.** The store announces its changes. Controllers react to them and to a resync loop. Foundry
  has no watch, so the observer lists and compares, and a partial listing never causes a deletion.
- **Scheduler framework.** PreFilter, Filter, Score, Reserve, Permit, Bind and Unreserve, with
  plugins and a profile per agent. The earlier strategies are score plugins. Applications can add
  plugins. The grant a cycle returns is released by one `finally`.
- **Optimistic concurrency.** `update` succeeds only against the current `resource_version`.
- **Reload.** `ConfigHolder` swaps in a validated configuration. A reload cannot add or remove
  agents, and a rejected one changes nothing and is visible as a condition.
- **Events and conditions in the admin API**, so an operator sees what the controllers did and why.
- **One process.** The controllers are tasks in the API process, not separate services.

Deliberately not adopted: a generic API server or CRDs, etcd, and leader election.

## Why one process

The store is in memory. Splitting the planes now would let two processes each believe they own the
same session. The boundaries between planes are interfaces (`SessionRegistry`, `AffinityStore`,
`QueueManager`, `FoundryAdapter`, the controller manager), so a split later does not need a redesign.

## Consequences

- Cleanup is correct by construction. A remote session cannot be forgotten, and a failed delete is
  visible and retried.
- Behaviour that tests used to reach through `Reconciler` still goes through it: a sync runs the
  controllers in a fixed order and waits for them. The reconciler is now an orchestrator.
- Creation of a warm session runs inside the pool controller's reconcile, which waits for the
  session to become ready. It is not a separate "desired session" resource. This keeps one owner for
  a creation and costs a worker for its duration.
- A session in use is never deleted. Deletion waits for the lease to end, and releasing the lease is
  what triggers it.
- Circuit breaker settings are read at start-up and are not reloaded.

## The way to scale out

In order, none of it done yet:

1. A durable store with compare-and-set transactions, behind the same ports.
2. Request and lease expiry that works across processes (reservations already expire by age).
3. Shared lease ownership, so no two processes serve the same session.
4. Leader election for the controller manager.
5. Then deploy API replicas and a controller manager separately, as `kube-controller-manager` is
   separate from the API server.
