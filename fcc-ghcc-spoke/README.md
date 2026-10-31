# fcc-ghcc-spoke

Source for the ServiceNow "GitHub Cost Center" spoke (scoped app `x_fcc_ghcc`) for FCC. See PRD v0.3 and the implementation plan for scope and phases.

This folder holds what can be built and tested without a ServiceNow instance:

| Path | Purpose |
| --- | --- |
| `lib/` | `GHCCClient`, the plan engine, validation rules, drift detection and anomaly checks. ES5 only, because it is packaged into a ServiceNow Script Include. |
| `mock/server.js` | Mock GitHub enterprise billing API: a synchronous handler plus an HTTP wrapper. |
| `test/` | Unit and mock-backed tests (`node:test`, no dependencies). |
| `ui/` | Clickable prototype of the seven screens (`ui/mockups/index.html`) and the build spec for UI Builder (`ui/SCREENS.md`). |
| `spikes/` | Phase 0 scripts (all eleven written) that record real GitHub responses. See `spikes/README.md`. |

The ServiceNow application files (tables, Script Include wrapper, REST Message, flows, ATF) are not created yet. They come from the instance through Studio source control; see "Next steps".

## Run

Needs Node 22 or later. No `npm install` is required.

```sh
npm test            # 186 tests; CI runs them (.github/workflows/fcc-ghcc-spoke.yml)
npm run mock        # mock API on http://127.0.0.1:4010, token "test-token"
```

Environment for `npm run mock`: `MOCK_PORT`, `MOCK_TOKEN`, `MOCK_SUPPORT_TEAMS=1` (accept teams as cost center resources) and `MOCK_AUTO_ASSIGN=1` (setting the `cost_center` repo property also assigns the repo); both stand in for Phase 0 findings. Library-level options (`supportAzureSubscription`, `supportBudgetExpiry`, `rateLimit`) are available to tests through `createMock`.

Test helpers on the running server: `POST /__mock/reset`, and `POST /__mock/fault` with `{ "status": 429, "headers": { "retry-after": "3" }, "method": "GET", "pathIncludes": "budgets", "times": 2 }`. ATF suites use these to exercise retry, throttling and error paths.

## Plan engine

`lib.plan(desired, observed, policy, today)` is a pure function: it compares desired state (from the ServiceNow tables) with observed state (from GitHub) and returns `{ operations, errors, warnings, blocked, summary }`. `lib.apply(plan, client, { requestNumber })` runs the operations through `GHCCClient`, one change-log entry per write. `lib.observe(client)` reads the observed state.

- **Inputs.** `desired`: `orgs`, `costCenters` (state `approved`, `draft` or `retired`; a missing state means approved), `fundingTeams`, `teamMembers`, `users`, `repos`, `budgets`, `moves`. `observed`: `costCenters` with their `resources`, and `budgets`. `policy`: `maxCostCenters`, `maxRemovals` (default 50), `teamsSupported`, `overrideCapByRole`, `defaultOverrideCap`, `totalOverrideCap`, `secondApproverThreshold`, `allowLicensedUnallocated`, `budgetScopeMap`. `today` is a required `YYYY-MM-DD` string; the engine never reads a clock, and `plan`, `detectDrift` and `detectAnomalies` throw without it, because a missing date would silently skip expiry and review checks.
- **Operations**, in the order they run: `create_cost_center`, `add_resources`, `move_resources`, `remove_resources`, `create_budget`, `update_budget`, `delete_budget`, `delete_cost_center`. Each has an `id` (`op-1`), a `description` for the plan screen, and a `githubId` where it targets an existing object.
- **Blocked plans.** Any validation error sets `blocked`; the operations still show what would happen, but `apply` refuses to run them. Rules covered: R1, R2, R3, R5, B5 caps and second approver, the cost center limit, budgets on unknown cost centers, $0 or non-protective budgets, overrides without an expiry, duplicates, and licensed users left unallocated. Warnings do not block.
- **Safety.** Resources are removed only from cost centers that say `managed: true` (the default is unmanaged), and only when no desired assignment exists for them anywhere. A resource that sits in another cost center is never moved without approval from both managers.
- **Removal guards.** A plan that would remove more than `policy.maxRemovals` resources, or would remove any while nothing is desired anywhere, is blocked (`too_many_removals`, `desired_state_empty`). These catch a partial or empty desired state, for example after a failed sync.
- **Approval and freshness.** `plan()` returns a `fingerprint` of its operations. `apply()` needs `meta.replan`, a function that observes GitHub again and returns a fresh plan; it refuses to run if the fresh operations differ from the approved ones. `meta.expectedFingerprint` rejects a plan edited after approval. `meta.allowStale` skips the check for tests and for remediation plans built a moment ago.
- **Re-running.** After a successful apply, planning again returns no operations. After a failed apply, the rest are marked skipped and re-planning picks up from where it stopped.

**Assumption to confirm:** which GitHub budget scope stands for "every licensed user" and "each user in one cost center" (`lib/budgetmap.js`). The defaults are guesses until the Phase 0 budget spike runs; `policy.budgetScopeMap` replaces them without code changes.

## Drift detection

`lib.detectDrift(desired, observed, options)` compares GitHub with what ServiceNow has **already applied** and returns one item per difference. `desired` must hold only applied records: an approved change that has not been applied yet is pending, not drift, and `plan()` handles it. `lib.reconcileDrift(detected, existing)` keeps the ServiceNow drift table free of duplicates across repeated syncs (X4).

| Drift type | Example | Fix offered |
| --- | --- | --- |
| `budget_changed` | Amount or stop usage edited in the GitHub UI | Revert, or accept as the new desired state |
| `membership_changed` | User or team removed from, or added to, a managed cost center | Add back or remove, after approval |
| `resource_moved` | A repo or user sits in a different cost center | A move request for both managers (R5); never an automatic fix |
| `unmanaged_object` | A cost center or budget exists in GitHub but not in ServiceNow | Adopt it as an unmanaged record |
| `missing_object` | An applied cost center or budget is gone from GitHub | Recreate it |
| `repo_property_drift` | A repo ServiceNow has a record for has a different or missing property | Accept or correct by hand |
| `org_baseline_missing` | No UNALLOCATED fallback, or the `cost_center` property is not defined | Create the fallback; the property is manual |

- **Modes (X1).** Each type has a mode per org: `report`, `approve` or `auto`. Everything starts in `report`. A mode above what a type allows is lowered, with a note: membership and moves stop at `approve`, and unmanaged objects, property drift and baseline problems stop at `report`. `lib.DRIFT_RECOMMENDED_MODES` holds the settings PRD 4.5 suggests once an org has run in report mode.
- **Org-scoped modes.** Per-org modes apply to drift that belongs to an org (repo properties, org baseline). Everything else uses the defaults.
- **Fixes run through `apply()`.** `item.remediation` is a plan-shaped object, so a revert is applied, change-logged and checked like any plan. It is `null` when nothing safe can be done automatically.
- **Not drift.** Resources inside a cost center that is itself missing are not listed separately; they return when it is recreated. Extras in a cost center that is not marked `managed: true` are ignored. A retired cost center that still exists is pending deletion.
- **Not yet read from GitHub.** `observe()` returns cost centers and budgets only. Repo properties and org property definitions are optional inputs (`observed.repoProperties`, `observed.orgProperties`) until the ServiceNow sync reads them, and alert recipients are not compared because the budget model does not carry them yet.

## Anomaly checks

`lib.detectAnomalies(desired, observed, usage, options)` looks for misconfigurations and unusual spend (PRD 4.6) and returns items with a stable `key`, a `check`, a `subject`, a `severity` and a plain-language `message`. `options.today` is required, because the engine never reads a clock. `lib.reconcileAnomalies(detected, existing)` keeps the anomaly table free of duplicates: open, dismissed and accepted anomalies are not raised again, resolved ones can recur.

| Check | Severity | What it looks at |
| --- | --- | --- |
| `licensed_user_unallocated` | high | A licensed user in an UNALLOCATED cost center, or in none, as GitHub sees it: a direct resource, else a team they belong to, else their org's placement |
| `user_in_teams_of_two_cost_centers` | high | A user in funding teams that belong to different cost centers |
| `cost_center_budget` | high | A cost center, including the UNALLOCATED fallback, with no budget or with stop usage off |
| `overlapping_budgets` | medium | More than one budget for the same scope, entity and user |
| `user_budget_zero` | medium | A per-user budget of $0 |
| `repo_property` | medium | A repo with no `cost_center` property or an invalid value, with a likely intended value proposed |
| `expired_override_active` | medium | An override past its expiry that is still in GitHub |
| `spend_spike` | configurable (default medium) | The latest day's spend against the daily average of the 28 days before it, per cost center and per user |
| `budget_threshold`, `pool_threshold` | alert | Month-to-date spend at 75, 90 or 100 percent of the budget or pool; only the highest level reached is reported, and a new level in the same month is a new alert |

- **Usage input.** `usage` is `{ daily: [{ date, costCenter, usd }], userDaily: [{ date, login, usd }], pools: { costCenter: limitUsd } }`, matching the `usage_daily` and `usage_user_daily` tables. Every part is optional; a check that needs missing data is skipped.
- **Spike settings.** `options.spike` takes `multiplier` (3), `baselineDays` (28), `minHistoryDays` (7), `minUsd` (25), `floorUsd` (5) and `severity`. Nothing is raised until `minHistoryDays` of history exist, and small amounts are ignored.
- **Thresholds.** `options.thresholds` defaults to `[75, 90, 100]`, which is what the PRD's alert levels use. GitHub's own alert thresholds are preset, so these alerts come from ServiceNow's calculations.
- **Not included.** Blocked-user detection (B6) needs the shape of GitHub's budget user states, which is unverified until the Phase 0 user-state spike runs.

## How the client is built

- **Required `sleep`.** The constructor throws without a `sleep(ms)` function (in ServiceNow, one that calls `gs.sleep`), so retries can never fire back to back.
- **Synchronous.** ServiceNow's `RESTMessageV2` is synchronous and its server runtime may lack Promises, so `GHCCClient` takes a synchronous `transport(req) -> { status, headers, body }`. Tests inject the mock's in-process handler; in ServiceNow the transport wraps a REST Message and adds the credential-alias token. The library never sees the token.
- **One write path.** Every write goes through `_write`: request number required, kill switch checked, `change_log` entry before and after, errors mapped. Create operations are idempotent (look up first).
- **Retry.** 429, 502, 503, 504 and rate-limited 403 retry (a 403 counts as rate limited when it has rate-limit headers or its message names a rate limit) with exponential backoff. A server-requested wait longer than `maxDelayMs` throws `throttled` and calls `onThrottle`, which the sync jobs use to pause. A 502, 503 or 504 on POST is not retried, because the object may have been created; `createCostCenter` and `createBudget` read back instead and report `recovered: true` if it exists.

## What is confirmed and what is assumed

`lib/endpoints.js` holds every URL and response key, with a `CONFIDENCE` table. In short:

- The base paths `/enterprises/{enterprise}/settings/billing/cost-centers` and `.../budgets` came from GitHub documentation search results.
- Everything else is assumed: the resource and user-states paths, response wrapper keys, pagination style, budget field names, and the `Team` resource type. The mock encodes the same guesses, so a passing test proves the client matches the mock, not GitHub.
- Phase 0 spikes replace guesses with recorded responses. Change `lib/endpoints.js` and `mock/server.js` together when they do.

## Known gaps

- Usage endpoints (usage summary, AI credit usage report, exports) are not implemented in the client or the mock.
- `removeResources` sends a body with `DELETE`. Confirm that `RESTMessageV2` can do this; if not, the transport needs a workaround.
- The cost center list is read page by page and the mock caps pages at 100, but whether GitHub paginates this endpoint, and how, is still an assumption for Phase 0. The mock does not model budget limits per account, audit logs or rate-limit headers other than injected faults.

## Next steps

1. Run the Phase 0 spikes against a throwaway enterprise and record responses in `spikes/findings/`.
2. Correct `lib/endpoints.js` and the mock from the recordings.
3. Add the plan engine and validation rules (PRD section 6) as pure functions in `lib/`, with tests.
4. Create the scoped app on a developer instance, link it to this repository through Studio, and add a build step that wraps `lib/` into the `GHCCClient` Script Include.
