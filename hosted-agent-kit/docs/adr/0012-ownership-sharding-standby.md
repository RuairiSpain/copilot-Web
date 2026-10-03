# ADR 0012: Exclusive ownership, sharding and standby instead of Raft

## Status
Accepted. Supersedes the one-kit-per-project assumption in ADR 0003.

## Context
A project may need several kits (teams, agent groups) and redundancy. Two kits scheduling the same
agent would both create sessions and exceed limits. A Raft cluster replicates state the kit can
rebuild from Foundry anyway.

## Decision
- Each agent, or shard of an agent, has exactly one owner kit, named by `kit_id` and `owns`.
- Ownership is a lease in Redis (claim, renew, release, with an epoch). Without Redis, ownership is
  checked by configuration only (`hack plan`).
- A kit that cannot renew in two thirds of the lease time stops serving.
- A standby takes over after the lease expires and waits a quiet period before serving.
- Large agents are split into shards by hashing the user id. Session ids carry the shard prefix.
- The state (sessions, leases to users) is rebuilt from Foundry listings and derived session ids
  (ADR 0007), so no replicated log is needed.

## Consequences
- Failover takes up to the lease time plus the quiet period.
- Ownership needs a reachable Redis in production. If it is down, owners self-fence and stop.
- Changing the shard count moves users to other shards; plan it as a migration.
- The standalone service does not use leases yet.
