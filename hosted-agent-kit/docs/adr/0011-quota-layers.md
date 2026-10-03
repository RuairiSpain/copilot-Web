# ADR 0011: Control the session quota in layers

## Status
Accepted.

## Context
Foundry limits concurrent sessions per subscription and region. Several kits (and other tools) may
share one limit. A shared counter that every call must reach would be a single point of failure,
and the platform's real count is the only exact one.

## Decision
Use four layers, each optional and each safe when the next is missing:
1. Read the platform's usage, if an API exists. Not implemented.
2. A static budget per kit, checked by `hack plan`.
3. An adaptive limit (increase by a step, decrease by a factor on a quota 429, with a cooldown).
4. A Redis ledger used only to borrow a spare pool and to show the regional total. The kit never
   waits for it: calls time out at 0.25 s and the kit carries on with layers 2 and 3.

A kit counts sessions locally (`QuotaGate`) and decides admission under one lock. At a limit it
stops the least recently used idle session (`stop_session`) rather than deleting it.

## Consequences
- No hard guarantee: another tool using the same quota can still cause a 429, which is then mapped
  to `SessionQuotaError` or `RegionalCapacityError` and lowers the limit.
- Budgets must be sized by people. `hack plan` finds errors but cannot choose them.
- Redis is an optional extra. Its failure reduces visibility and borrowing only.
