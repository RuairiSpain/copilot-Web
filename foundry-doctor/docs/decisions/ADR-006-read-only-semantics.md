# ADR-006: What "read-only" means for Azure checks

Status: Proposed (needs the Azure black belt review)
Date: 2026-10-04

## Context

The PRD and `CLAUDE.md` say the doctor is read-only and never mutates live resources. Phase 0 research for the DEP and
RUN groups (`docs/overlap/dep.md`, `docs/overlap/run.md`) found checks that use HTTP POST without changing anything, and one
check that needs deployment-level permission:

| Check | Call | Changes resources | Reader enough |
|---|---|---|---|
| FND-DEP-007 name availability (Key Vault, Storage, APIM) | POST `checkNameAvailability` | No | Yes, per the DEP research |
| FND-DEP-007 Foundry `checkDomainAvailability`, Search `checkNameAvailability` | POST | No | No (`/action` permission) |
| FND-DEP-009 policy restrictions | POST `checkPolicyRestrictions` | No | Yes (`checkPolicyRestrictions/read`) |
| FND-DEP-008 ARM what-if | POST `Microsoft.Resources/deployments/whatIf` | Documented as no changes to existing resources | No (needs `whatIf/action`, same requirements as deploying) |
| FND-RUN-004/007 metrics and Log Analytics queries | GET and POST `/query` | No | Mostly |

The DEP agent could not verify whether a what-if leaves a deployment record or activity-log entry, so no claim is made either way.

## Decision

1. **Definition.** A check is read-only if it cannot create, modify, delete, start, stop, purge or recover an Azure resource,
   and cannot read secrets, keys, document content, prompts or completions. The HTTP verb is not the test.
2. **Allowed POSTs.** POST is allowed only for operations documented as evaluation or query. Each allowed operation is listed
   in `docs/permissions-matrix.md` (a Phase 2 deliverable) with verb, path, api-version and required permission.
   The Azure client interface exposes only these operations by name; there is no generic "send request" method.
3. **Name checks and Search/Foundry availability checks** use Reader where Reader suffices. Where an `/action` permission is
   needed and the identity lacks it, the check is reported skipped, naming the missing permission, never passed.
4. **What-if is opt-in.** It runs only with an explicit `--what-if` (or `--preflight --what-if`), never by default and never
   in the same invocation as a real deployment. It is described in the output as "non-mutating but needs deployment-level
   permission". If the permission is missing the check is skipped.
5. **Forbidden regardless of verb:** recover, purge, register-provider, lock create or delete, role assignment writes,
   listing credentials or keys, and any data-plane call that returns document, prompt or completion bodies.
6. A unit test in `internal/azure` fails if the client interface gains a method not in the allow-list, and a replay test fails if
   a recorded request uses a verb or path outside it.

## Consequences

- `CLAUDE.md` wording changes from "never mutates Azure" to this definition when Phase 2 starts.
- Phase 2 needs a permission matrix per check, including which checks degrade to skipped under Reader only.
- If Microsoft documents that what-if leaves a persistent record, the opt-in rule stands and the output wording changes.

## Unverified

- Whether what-if writes a deployment or activity-log entry.
- Whether `checkPolicyRestrictions` honours policy exemptions.
- Which built-in role grants the two `/action` name-check permissions.
