# Session quota, kits and ownership

Foundry limits concurrent hosted-agent sessions per subscription and region. The kit helps you stay
under that limit and shows what happens when you do not. Everything here is optional: with no
`quota` settings the kit enforces nothing and only reports.

> The platform facts below come from public documentation summaries and were not confirmed against
> the service. Confirm them before relying on them (see [Open questions](#open-questions)).

## What counts

The kit counts a session as holding compute while it is leased, being created or updated, or has
been active within `idle_timeout_seconds` plus a 30 second margin. A session the kit stopped
(`stop_session`) stops counting at once. A stopped session keeps its state and can resume.

## Layers

1. **Platform usage lookup.** Not implemented: no usage API is known.
2. **Static budgets.** `quota.budget` is the most sessions one kit may use. `hack plan` checks all
   kits' budgets against `quota.region_limit` before deployment.
3. **Adaptive limit.** A `session_quota_exceeded` or `regional_session_quota_exceeded` answer
   multiplies the limit by `decrease_factor` (not below `min_limit`). After `probe_seconds` without
   a refusal the limit rises by `increase_step` (at least 5% of the budget), up to the budget.
   Refusals within `cooldown_seconds` of a decrease are ignored.
4. **Shared ledger (optional).** With `quota.ledger` (Redis), kits publish their counts. A kit may
   borrow permits from the spare pool (`quota.spare`) and reporting shows the regional total.
   A permit that lapses in Redis (no heartbeat within `ledger.ttl_seconds`) is dropped by the kit
   at its next heartbeat, or locally when Redis stays unreachable for that long.
   If Redis is down the kit keeps working on layers 2 and 3. A 429 caused by another tool that
   shares the quota also lowers this kit's limit.

At an active limit the kit stops the least recently used idle session to make room. If none is idle
the call waits in the queue.

## Ownership

Only one kit may schedule an agent, or one shard of an agent. `kit_id` names the kit and `owns`
lists its agents. `hack plan` reports overlaps, shard gaps and duplicate kit ids.

- **Sharding.** `shard: {index, count}`. A user maps to a shard by SHA-256 of the user id.
  Session ids carry the prefix `pool-s{index}-`. A call for another shard's user raises
  `WrongShardError` (421).
- **Leases.** With `ownership.backend: redis` a kit holds one lease per agent (and shard), renewed
  every `renew_seconds` (at most a third of `ttl_seconds`). A kit stops serving (self-fencing) when
  its next renewal attempt would fall after two thirds of the lease time, so it fences before
  another kit can take the lease. The epoch only protects renewals: Foundry calls carry no fencing
  token, so calls already in flight finish during the shutdown grace.
  Leases are renewed while a kit starts up, and a standby kit starts even if the store is
  unreachable and keeps trying. The Redis scripts need Redis 5 or later and a primary node.
- **Standby.** With `ownership.standby: true` a second kit waits and takes over after the lease
  expires, then waits `quiet_seconds` before it admits calls.
- **No Raft.** State is rebuilt from Foundry on takeover, so no consensus cluster is needed.
  See ADR 0012.

## Reading the state

`kit.reporting.quota()`, `GET /v1/admin/quota` and `GET /quota` (FastAPI router) return counted
sessions, the limit, borrowed permits, refusals, evictions and, with a ledger, the regional view.
Metrics: `quota_refused`, `session_evicted`, `sessions_counted`.

## Open questions

1. Is the limit per subscription and region, and does it count only provisioning and running
   sessions?
2. Do the two error codes keep those names and meanings?
3. Is the idle timeout 15 minutes by default, 2 to 60 minutes configurable? (The kit accepts 1 to
   240 minutes in `idle_timeout_seconds`.)
4. Are `x-ms-user-isolation-key` and `x-ms-user-identity` the right header names?
5. Is there a usage API the kit can read (layer 1)?
6. Does the standalone service need ownership leases? It does not use them today.
