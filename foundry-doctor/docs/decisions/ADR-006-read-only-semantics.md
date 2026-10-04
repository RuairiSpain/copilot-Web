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
   a recorded request uses a verb, host or path template outside it. The allow-list covers data-plane Search paths and
   Log Analytics queries as well as ARM.
7. **Log Analytics and Application Insights queries are fixed templates.** Log tables can hold prompt and completion bodies
   (FND-SEC-012 documents APIM LLM logging). Queries ship in the catalogue, are aggregate-only (`summarize`/`count`), project
   only an allow-listed set of columns, and take the resource name only after it passes the ARM name charset check
   (`[A-Za-z0-9._-]{1,64}`) and as a parameter, never by string concatenation. The shipped template is the only query text;
   the client rejects any supplied or interpolated KQL that contains `take`, `top`, `message`, `*` or a projection outside the
   allow-list. A replay test with a hostile resource name must fail closed.
8. **Response payloads that can carry secrets or content are discarded at the client boundary, never logged or persisted:**
   `properties.outputs` and `properties.parameters` of deployments (FND-DEP-010), the `before` and `after` bodies of what-if
   results (FND-DEP-008, requested as `FullResourcePayloads` only to read `changeType`), the `resourceContent` we send to
   `checkPolicyRestrictions` (built from non-secret template content only, FND-DEP-009), and the free-text error strings of
   Search indexers (FND-RUN-006, reduced to an error code and counts before storage).
9. **"Reader suffices" is a hypothesis until tested.** The rows marked Reader in the DEP tables rest on RBAC action names alone.
   The Phase 2 replay suite must run every such row with a Reader-only identity before the permission matrix says Reader suffices.
10. **Remediation text is output only.** Fix examples that contain delete or purge commands (for example in FND-DEP-006) are
    printed for a human; the tool never executes them.

## Consequences

- `CLAUDE.md` wording changes from "never mutates Azure" to this definition when Phase 2 starts.
- Phase 2 needs a permission matrix per check, including which checks degrade to skipped under Reader only.
- If Microsoft documents that what-if leaves a persistent record, the opt-in rule stands and the output wording changes.

## Unverified

- Whether what-if writes a deployment or activity-log entry.
- Whether `checkPolicyRestrictions` honours policy exemptions.
- Which built-in role grants the two `/action` name-check permissions.
