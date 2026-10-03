# Architecture

## Two ways to run it

The scheduler is a library first (ADR 0010). `Runtime` (`runtime.py`) wires the pool service, the
controllers, the stores and the metrics. Two front ends use it:

- **SDK**: `Hack` (`kit.py`) wraps a `Runtime`. Your FastAPI application imports it and calls it in
  process. `integrations/fastapi.py` adds lifecycle, error handling, streaming and routers.
- **Standalone service**: `hosted_agent_kit.service` composes a `Runtime` and adds Entra
  authentication, request limits, idempotency headers and HTTP routes.

Everything below describes the runtime, which is the same in both.

## Where the code runs (standalone service)

A standalone, containerised FastAPI service. It sits between client applications and Foundry hosted
agents. It runs on Azure Container Apps pinned to one replica, as one process.

```
 Client app ──Entra bearer token──▶ Pooling service ──Managed identity──▶ Foundry project
                                     │                                      hosted agent sessions
                                     │  Request plane   API ▸ scheduler ▸ reserve ▸ invoke ▸ release
                                     │  State plane     session store, pool resources, events
                                     └─ Control plane   controllers that converge state with Foundry
```

The service follows Kubernetes control-plane patterns, in one process and without Kubernetes (see
[ADR 0009](adr/0009-control-plane-architecture.md)). The request path reserves and uses sessions. It
does not own their lifecycle: controllers converge sessions and pools toward the desired state and
clean up after failures.

## The three planes

| Plane | Parts | Role |
|---|---|---|
| Request | API, `PoolService`, scheduling framework | Match a request to a session, reserve it, invoke the agent, release it. |
| State | `SessionRegistry`, `PoolStore`, `EventRecorder` | The resources and their conditions. The only place the planes meet. |
| Control | `ControllerManager` and the controllers | Observe Foundry and the store, and converge them. |

## Resources

Every resource has a key, a `generation` that changes when its desired state changes, a
`resource_version` that changes on every stored update, and `conditions` that say what is true of it
and why.

| Resource | Key | Desired (spec) | Observed (status) |
|---|---|---|---|
| `AgentPool` | agent name | The agent's configuration. | `observed_generation`, session counts, conditions. |
| `AgentSession` | `(agent name, session id)` | Affinity binding, deletion request. | Platform status, local state, lease, conditions. |

A session id is unique only within one agent endpoint, so every key has both parts.

**Pool conditions:** `Ready`, `InitialSyncComplete`, `CapacityAvailable`, `FoundryReachable`,
`CircuitOpen`, `ConfigurationValid`, `RecoveryBlocked`. **Session conditions:** `Provisioned`,
`Ready`, `Leased`, `DeletionPending`, `DeletionFailed` (and `Recoverable`, computed for the admin
API). `generation` against `observed_generation` shows a configuration change that has not taken
effect yet. A condition's transition time moves only when its status changes.

**Finalizers.** A session marked for deletion keeps `foundry-session-cleanup` until Foundry confirms
the remote session is gone, and `foundry-platform-deletion` while Foundry itself reports it
deleting. The record is removed only when no finalizer is left. A remote session is therefore never
forgotten, whichever path asked for its deletion.

**Optimistic concurrency.** `update` succeeds only if the caller read the current
`resource_version`, so a stale read cannot overwrite newer state. `mutate_session` retries on a
conflict. The in-memory store never has a conflict that an `await`-free call could not avoid, but
the contract is what a shared store will need.

**Watch.** The store announces every change (`Added`, `Modified`, `Deleted`, with a snapshot) to
subscribers. A change that only updates conditions is marked `status_only`, so a controller does not
react to its own status writes.

## Layers (ports and adapters)

| Layer | Package | Role |
|---|---|---|
| Domain | `domain/` | Enums, models, resources and errors. No I/O. |
| Ports | `ports/` | Interfaces: Foundry, session store, affinity store, queue, metrics. |
| Services | `services/` | `PoolService`, scheduling framework, remote-session operations, `PoolStore`, events, idempotency. |
| Controllers | `controllers/` | Work queue, controller manager and the controllers. |
| Adapters | `adapters/` | `SdkFoundryAdapter` (Azure SDK), in-memory stores, OpenTelemetry. |
| API | `api/`, `main.py` | Routes, middleware, problem+json, health, app factory. |
| Security | `security/` | Token validation, principal, role checks. |

The services and controllers depend only on ports. Tests replace the Foundry adapter with an
in-memory fake, and the stores could be replaced by a shared store without touching them (ADR 0001,
0003 and 0009).

## Controllers

All run in the one process as asynchronous tasks owned by the `ControllerManager`. A controller
reconciles one resource key at a time and must be idempotent.

| Controller | Resource | Does |
|---|---|---|
| `session` | `AgentSession` | Deletes the remote session, holding the finalizer until it is confirmed; retries with backoff; removes the record. |
| `observation` | `AgentPool` | Lists Foundry, compares with the last observation, adopts or ignores unknown sessions, marks missing or failed ones for deletion, never infers a deletion from a partial listing. |
| `pool` | `AgentPool` | Tops up warm sessions to `min_warm_sessions`, retires idle sessions on another agent version. Acts only on a complete observation. |
| `gc` | `AgentPool` | Hands back sessions whose deletion is pending, gives up on sessions stuck provisioning, drops affinity entries for sessions that are gone, expires capacity reservations nobody released. |
| `health` | `AgentPool` | Records `FoundryReachable` and `CircuitOpen` from the circuit breaker and the observer. |
| `status` | `AgentPool` | Writes counts, `observed_generation` and the other conditions, including `Ready`. |

**Work queues.** Each controller has a keyed queue. A key is queued at most once however often it
is added; a key added while it is being processed is queued again once when it finishes; failures
back off exponentially up to a retry limit; `requeue_after` schedules a key for later. This is
separate from the request queue, which holds callers waiting for a session.

**Triggers.** The store's change events queue the affected keys (a session marked for deletion, a
session removed, any change for status). A resync loop per agent runs the controllers in a fixed
order every `sync_interval_seconds` and on `POST .../sync`: observation, health, garbage
collection, pool, status. Foundry has list and get but no watch, so the observer lists and
compares. A partial listing never produces a deletion.

**Not a separate service.** The controllers share a process with the API because the store is in
memory. Splitting them needs a shared transactional store, leases and leader election first (ADR
0003 and 0009).

## Session model

Each pooled session has a local record:

| Local state | Meaning |
|---|---|
| `available` | Active or idle in Foundry and not leased. Can be scheduled. |
| `leased` | Held by exactly one request. |
| `unavailable` | Provisioning, updating or status unknown. Not scheduled. |
| `retiring` | Marked for deletion (`deletion_timestamp` set). Never scheduled. A lease in progress finishes first. |

Foundry statuses map as follows: `active` and `idle` become available. `creating`, `updating` and
unknown values become unavailable. `failed` is deleted. `deleting` becomes retiring and waits for
Foundry. `deleted` and `expired` are removed locally without a remote call.

A session is recorded the moment Foundry has created it, before it is ready, so something always
owns it. If it never becomes usable, or the request is cancelled, it is marked for deletion and the
session controller removes it.

## Scheduling

A request is matched to a session by a scheduling cycle with the phases of the Kubernetes
scheduler. The caller holds the agent's lock, so cycles never interleave.

| Phase | Does |
|---|---|
| PreFilter | `AffinityResolution`: another request is creating this user's session (wait), the user is bound to a healthy session (it is the target), the binding is stale (drop it), or a restored session is waiting under the user's derived id. |
| Filter | Removes sessions that cannot serve it. Core filters always run: `Ready`, `AffinityCompatible`, `RestoreHeld`. Optional: `VersionCompatible`, `NotExpiring`. |
| Score | Ranks the rest. A strategy score (`FirstAvailable`, `RoundRobin`, `OldestIdle`, `NewestIdle`), or weights over `AffinityPreference`, `VersionPreferred`, `ExpiryRisk`. |
| Reserve | Leases the best session, or reserves a capacity slot (and a pending affinity claim) to create one. |
| Permit | A request whose own session is busy waits for that one, never for another. With no room, it waits for anyone. |
| Bind | Records the affinity on the session and in the affinity store. |
| Unreserve | Gives back a lease, a slot and a claim. Idempotent. |

A profile per agent lists the filters and score weights. `scheduler_profile.filters` adds to the core
filters. `scheduler_profile.scores` replaces the strategy score. An unknown plugin name stops the
service at start-up. Applications can add their own plugins with a `PluginRegistry` passed to
`create_app`. Every cycle ends in a grant (a lease or a slot) or a wait, and the grant is released by
a single `finally` in the request path.

## Request flow

1. Authenticate, check the role and resolve the user id (the `oid` claim).
2. Reject unknown agents with 404 before any other work. Refuse the request if the service is
   shutting down.
3. Run a scheduling cycle (above). If it waits, queue, if the queue is enabled and not full, else
   fail with 429.
4. Create the session if needed, recording it at once, and wait until Foundry reports it active.
5. Invoke the agent with the protocol the agent uses, binding the session to the request.
6. Release the lease in a shielded `finally`, then wake the next waiter.

The queue is FIFO among the requests that can be served. A waiter pinned to a busy stateful session
is skipped, so it never blocks a later caller who could run now. A dispatcher resolves a waiter's
future with a session grant or a creation grant. A waiter that times out or disconnects removes
itself. If its future was already resolved, it hands the grant back, so a session or slot is never
stranded.

## Shutdown

On shutdown the service marks itself not ready, stops admitting requests (new ones get
`503 SERVICE_SHUTTING_DOWN` with `Retry-After`), waits up to `POOL_SHUTDOWN_GRACE_SECONDS` for
running requests and streams to finish, then stops the controllers (letting a running reconcile
finish) and closes the Foundry client.

## Protocols

The adapter speaks two protocols, chosen per agent. Responses goes through the OpenAI-compatible
client that the SDK provides. Invocations has no typed SDK client, so the adapter posts through the
SDK's own HTTP pipeline (`send_request`), which keeps authentication and the retry policy. In both,
the session is bound to the request (body field or query parameter) and removed from what callers
see.

## Circuit breaker

`CircuitBreakingAdapter` wraps the Foundry adapter. Every call for an agent goes through that
agent's breaker. An open circuit fails fast with `FoundryCircuitOpen`, which the pool maps to
`503 FOUNDRY_CIRCUIT_OPEN`. It is never retried. The reconciler uses the same wrapper, so a sync
during an outage is `incomplete`.

## Conversations and callers

A stateful session belongs to an affinity key: the user, the agent and optionally a conversation key.
The same user with another conversation key gets another session, and with derived ids (below) each
conversation has its own derived id. An app-only token is not a person, so for stateful agents the
service requires the end user in `X-Pool-Subject` and scopes the session to the calling service and
that subject.

## Idempotency

`IdempotencyStore` keys a response by caller, agent and `Idempotency-Key`, with a digest of the
request. The first request runs, a repeat replays the stored response, a repeat that is still running
is refused, and a changed request is an error. Only successful non-streaming responses are kept, for a
bounded time and size.

## Agent versions

New sessions are created on `agent_version`, or the latest version. With `version_drain` set, the
reconciler retires idle sessions on any other version. A leased session is never touched, and a
session that belongs to a user is kept unless the policy is `idle`.

## Restoring sessions

With `POOL_SESSION_ID_KEY` set, a stateful user's session id is derived from the user and agent.
The reconciler adopts sessions in that format and the pool lets only the matching user claim one.
See [configuration](configuration.md#restoring-stateful-sessions-after-a-restart).

## Failure handling

| Event | Behaviour |
|---|---|
| Circuit open | Fail at once with 503 and `Retry-After`. No retry, no Foundry call. |
| Foundry 429 | Retry on the same lease. `Retry-After` is a minimum delay. If it is longer than `backoff_max_seconds`, return `UPSTREAM_THROTTLED` with Foundry's delay instead of retrying early. |
| Session not found upstream | Drop the mapping, create a replacement and retry once. A second miss returns 502. |
| Session failed | Delete it. Retry once with a new session only when the caller sent `Idempotency-Key`. |
| Creation fails | Retry transient errors up to `create_retries`. A session that was created but did not become ready, or whose request was cancelled, is deleted. If that delete fails it is kept as `retiring` for the next sync. Give the slot back. Clear the pending claim. |
| Delete fails | The session keeps its finalizer and a `DeletionFailed` condition. The session controller retries with backoff and every sync retries. |
| Client disconnects | Cancel the work, release the lease, remove any queue entry. Respond 499. |
| Stream fails before the first frame | Return a normal problem response, release the lease. |
| Stream fails after the first frame | Send an SSE `error` event, release the lease. A failing close of the upstream stream is logged and never keeps the lease. |
| Leased session missing from a complete listing | Verify it, mark it `retiring` and keep the lease holder. It is removed when the lease ends and never offered again. |

## Reconciliation

On start-up, on each agent's interval and on demand, the observation controller lists the agent's
sessions and applies the status mapping above. It adds sessions it did not know about, which is how a stateless pool
recovers after a restart. Stateful agents skip unknown sessions unless `adopt_unbound_sessions` is
true, so a user can never inherit another user's session. The exception is a session whose id was
derived by this service (see above), which is held for its owner. The service reports ready once
every agent has had a first sync attempt, so users do not arrive before sessions are recovered. A listing that fails part-way is treated as incomplete: the service marks
unseen sessions unavailable and never infers deletion. A session missing from a complete listing is
verified with a single `get_session` before it is removed. Leased sessions are left alone and
retired after their lease ends. Warm sessions are then topped up to `min_warm_sessions`, never
beyond `max_sessions`.

## Telemetry

Structured JSON logs carry `correlation_id` and `request_id`. Metrics have only an `agent` label and
bounded labels such as `outcome`. User ids and session ids are never metric dimensions. Per-agent
`telemetry.enabled: false` stops business metrics for that agent. Operational logs continue.

## Why one replica, and the way past it

Leases, affinity, queues and the controllers' state live in process memory. Two replicas would each
believe they own every session. See [ADR 0003](adr/0003-single-replica-in-memory-state.md).

The control-plane structure is what a later scale-out needs, and ADR 0009 lists the steps:
a durable store with compare-and-set (the contract exists as `update` with `expected_version`),
request and reservation expiry across processes, shared lease ownership and leader election for the
controllers. Only then could API replicas and a controller manager be deployed separately.
