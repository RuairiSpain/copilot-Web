# fcc-ghcc-spoke

Source for the ServiceNow "GitHub Cost Center" spoke (scoped app `x_fcc_ghcc`) for FCC. See the PRD v0.2 and the implementation plan for scope and phases.

This folder holds what can be built and tested without a ServiceNow instance:

| Path | Purpose |
| --- | --- |
| `lib/` | `GHCCClient`, the plan engine and validation rules. ES5 only, because it is packaged into a ServiceNow Script Include. |
| `mock/server.js` | Mock GitHub enterprise billing API: a synchronous handler plus an HTTP wrapper. |
| `test/` | Unit and mock-backed tests (`node:test`, no dependencies). |
| `ui/` | Clickable prototype of the seven screens (`ui/mockups/index.html`) and the build spec for UI Builder (`ui/SCREENS.md`). |
| `spikes/` | Phase 0 scripts (all eleven written) that record real GitHub responses. See `spikes/README.md`. |

The ServiceNow application files (tables, Script Include wrapper, REST Message, flows, ATF) are not created yet. They come from the instance through Studio source control; see "Next steps".

## Run

Needs Node 22 or later. No `npm install` is required.

```sh
npm test            # 101 tests
npm run mock        # mock API on http://127.0.0.1:4010, token "test-token"
```

Environment for `npm run mock`: `MOCK_PORT`, `MOCK_TOKEN`, `MOCK_SUPPORT_TEAMS=1` (accept teams as cost center resources) and `MOCK_AUTO_ASSIGN=1` (setting the `cost_center` repo property also assigns the repo); both stand in for Phase 0 findings. Library-level options (`supportAzureSubscription`, `supportBudgetExpiry`, `rateLimit`) are available to tests through `createMock`.

Test helpers on the running server: `POST /__mock/reset`, and `POST /__mock/fault` with `{ "status": 429, "headers": { "retry-after": "3" }, "method": "GET", "pathIncludes": "budgets", "times": 2 }`. ATF suites use these to exercise retry, throttling and error paths.

## Plan engine

`lib.plan(desired, observed, policy, today)` is a pure function: it compares desired state (from the ServiceNow tables) with observed state (from GitHub) and returns `{ operations, errors, warnings, blocked, summary }`. `lib.apply(plan, client, { requestNumber })` runs the operations through `GHCCClient`, one change-log entry per write. `lib.observe(client)` reads the observed state.

- **Inputs.** `desired`: `orgs`, `costCenters` (state `approved`, `draft` or `retired`; a missing state means approved), `fundingTeams`, `teamMembers`, `users`, `repos`, `budgets`, `moves`. `observed`: `costCenters` with their `resources`, and `budgets`. `policy`: `maxCostCenters`, `teamsSupported`, `overrideCapByRole`, `defaultOverrideCap`, `totalOverrideCap`, `secondApproverThreshold`, `allowLicensedUnallocated`, `budgetScopeMap`. `today` is a `YYYY-MM-DD` string; the engine never reads a clock.
- **Operations**, in the order they run: `create_cost_center`, `add_resources`, `move_resources`, `remove_resources`, `create_budget`, `update_budget`, `delete_budget`, `delete_cost_center`. Each has an `id` (`op-1`), a `description` for the plan screen, and a `githubId` where it targets an existing object.
- **Blocked plans.** Any validation error sets `blocked`; the operations still show what would happen, but `apply` refuses to run them. Rules covered: R1, R2, R3, R5, B5 caps and second approver, the cost center limit, budgets on unknown cost centers, $0 or non-protective budgets, overrides without an expiry, duplicates, and licensed users left unallocated. Warnings do not block.
- **Safety.** Resources are removed only from cost centers marked managed, and only when no desired assignment exists for them anywhere. A resource that sits in another cost center is never moved without approval from both managers.
- **Re-running.** After a successful apply, planning again returns no operations. After a failed apply, the rest are marked skipped and re-planning picks up from where it stopped.

**Assumption to confirm:** which GitHub budget scope stands for "every licensed user" and "each user in one cost center" (`lib/budgetmap.js`). The defaults are guesses until the Phase 0 budget spike runs; `policy.budgetScopeMap` replaces them without code changes.

## How the client is built

- **Synchronous.** ServiceNow's `RESTMessageV2` is synchronous and its server runtime may lack Promises, so `GHCCClient` takes a synchronous `transport(req) -> { status, headers, body }`. Tests inject the mock's in-process handler; in ServiceNow the transport wraps a REST Message and adds the credential-alias token. The library never sees the token.
- **One write path.** Every write goes through `_write`: request number required, kill switch checked, `change_log` entry before and after, errors mapped. Create operations are idempotent (look up first).
- **Retry.** 429, 502, 503, 504 and rate-limited 403 retry with exponential backoff. A server-requested wait longer than `maxDelayMs` throws `throttled` and calls `onThrottle`, which the sync jobs use to pause.

## What is confirmed and what is assumed

`lib/endpoints.js` holds every URL and response key, with a `CONFIDENCE` table. In short:

- The base paths `/enterprises/{enterprise}/settings/billing/cost-centers` and `.../budgets` came from GitHub documentation search results.
- Everything else is assumed: the resource and user-states paths, response wrapper keys, pagination style, budget field names, and the `Team` resource type. The mock encodes the same guesses, so a passing test proves the client matches the mock, not GitHub.
- Phase 0 spikes replace guesses with recorded responses. Change `lib/endpoints.js` and `mock/server.js` together when they do.

## Known gaps

- Usage endpoints (usage summary, AI credit usage report, exports) are not implemented in the client or the mock.
- `removeResources` sends a body with `DELETE`. Confirm that `RESTMessageV2` can do this; if not, the transport needs a workaround.
- The mock does not model the 100-per-page cap on cost centers, budget limits per account, audit logs or rate-limit headers other than injected faults.

## Next steps

1. Run the Phase 0 spikes against a throwaway enterprise and record responses in `spikes/findings/`.
2. Correct `lib/endpoints.js` and the mock from the recordings.
3. Add the plan engine and validation rules (PRD section 6) as pure functions in `lib/`, with tests.
4. Create the scoped app on a developer instance, link it to this repository through Studio, and add a build step that wraps `lib/` into the `GHCCClient` Script Include.
