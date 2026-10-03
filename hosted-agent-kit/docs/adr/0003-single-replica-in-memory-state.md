# ADR 0003: Single replica, in-memory state and restart behaviour

Status: accepted

## Context

Leases, affinity maps and queues are held in process memory. Foundry scopes sessions by an
isolation key, which the platform takes from the caller's token. The pool uses one identity, so every
session shares one key and Foundry cannot tell the pool's users apart. The user-to-session mapping
therefore exists only in this process, unless the service encodes it in the session id (ADR 0007).

## Decision

- Run exactly one replica (`minReplicas = maxReplicas = 1`) with one worker process.
- After a restart, rebuild the session registry from Foundry. **Stateless** agents adopt every
  existing session.
- **Stateful** agents with `POOL_SESSION_ID_KEY` set recover the mapping: they adopt sessions whose
  id the service derived, and hold each one for the user it belongs to (ADR 0007).
- **Stateful** agents without the key do not adopt sessions they did not create (`adopt_unbound_sessions` defaults
  to `false` for them). Foundry sessions persist `$HOME` and uploaded files, so handing an unknown
  session to a new user could expose another user's data. Ignored sessions are not deleted, because
  they may belong to someone else, and they do not count against `max_sessions`.

## Consequences

- A restart interrupts in-flight requests. Users of stateful agents get their sessions back when
  the key is set, and fresh sessions when it is not.
- Orphaned stateful sessions remain in Foundry until they expire or an operator deletes them.
- Circuit breaker state (ADR 0006) is also per process and resets on restart.
- Setting `adopt_unbound_sessions: true` on a stateful agent restores PRD-style adoption. Use it only
  when the agent's sessions hold no user data.
- Scaling out needs a shared registry, affinity store and queue (a V2 change), plus a way to store
  session ownership.

See also [ADR 0009](0009-control-plane-architecture.md), which structures the service as planes and
controllers so that moving to a shared store later does not need a redesign.
