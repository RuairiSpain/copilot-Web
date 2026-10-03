# ADR 0007: Derived session ids to restore affinity after a restart

Status: accepted

## Context

Stateful agents keep user data in the session. After a restart the pool no longer knows which
session belongs to which user, and the safe default is to ignore every session it did not create
(ADR 0003). Users lose their sessions even though Foundry still holds them.

Foundry stores no user identity with a session that the pool can read: the isolation key comes from
the pool's own token. It does let the caller choose the session id when creating a session, if the id
is unique within the agent endpoint.

## Decision

- With `POOL_SESSION_ID_KEY` set, a stateful user's session is created under
  `pool-` + the first 40 hex characters of HMAC-SHA256(key, agent name, NUL, user id).
- The reconciler adopts sessions in that format and holds each one for its owner. They are not
  schedulable for anyone else.
- A returning user's id is derived again. Only the matching user can take that session, so one user
  cannot take another's. A user who arrives before the first sync finishes is served by a direct
  lookup of the derived id.
- A keyed hash, not the plain user id, so ids are unguessable and a reader of the Foundry portal
  cannot link a session to a user.
- With a key set, stateful agents never take unbound or warm sessions, because those ids cannot be
  changed. Start-up refuses `min_warm_sessions > 0` on a stateful agent in that case.
- A derived session that is failed, deleting, deleted or expired is replaced under a random id,
  because the old id may not be reusable yet. A conflict on creation also falls back to a random id.
- Readiness waits for one sync attempt per agent, so most users arrive after recovery.

## Consequences

- Sessions survive a restart for users who return, with no state outside Foundry.
- Rotating the key orphans existing sessions. They then expire after 30 days.
- A replacement under a random id is not recoverable after a restart.
- Sessions held for users who never return still count against `max_sessions`.
- Whether Foundry reuses a deleted id, and its answer to a duplicate id, are unverified. The
  fallbacks above cover both.
- Scaling out later would still need a shared registry, affinity store and queue. Derived ids make
  the affinity part of that easier.
