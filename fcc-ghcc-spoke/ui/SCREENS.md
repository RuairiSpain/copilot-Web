# ServiceNow screens: build spec

`mockups/index.html` is a clickable prototype of the seven screens in PRD section 4.7, plus the "Request more credits" form. Open it in a browser; use "View as" to see what each role gets. It uses invented sample data and is not a ServiceNow artifact: it contains no UI Builder pages, only the layout, behaviour and rules to build them from.

Build as one UI Builder Workspace experience, **GitHub Cost Center**, with one page per screen. Table names below omit the `x_fcc_ghcc_` prefix.

## Who sees what

| Screen | admin | cc_manager | team_manager | finance | auditor |
| --- | --- | --- | --- | --- | --- |
| Overview | write | own centers | | read | |
| Cost center admin | write | own centers, can approve | | | |
| Repo mapping | write | own centers, can request | | | |
| Requests | write | own centers, can approve | can request | | |
| Drift and anomalies | write | own centers, can approve | | | read |
| Chargeback | read, export | | | read, export | |
| Sync health | write (kill switch) | | | | read |

Scoping to "own centers" is an ACL with a row condition (`cost_center.manager = current user`), not a client filter. Hiding a button is a convenience only; every write action re-checks the role on the server.

## Screens

| Screen | Data resources | Main components | Actions (flow actions backed by `GHCCClient`) |
| --- | --- | --- | --- |
| Overview | `cost_center`; month-to-date sum of `usage_daily.usd` per cost center; `budget` at cost center scope; top five by `usage_user_daily`; thresholds from `policy` | Four tiles, table with a used-of-budget meter and 75, 90 and 100 percent marks, top users table | None |
| Cost center admin | `cost_center`, `funding_team`, `user_map`, `budget` filtered to the selected center; **plan** from a scripted data resource that runs the plan engine over Draft records | List, tabbed detail form, plan panel with a validation banner | Submit for approval (creates the approval; GitHub is written only after approval) |
| Repo mapping | `repo_map` with filters for org, cost center, and no-cost-center | Filter bar, table with row selection, sticky bulk bar | Create one request for the selected repos |
| Requests | `request` by type; approvals; `policy` for caps | Tabs, queue, detail with policy checks, catalog form | Approve (create or raise the individual override), Reject |
| Drift and anomalies | `drift`, `anomaly` | Tabs, table with desired and observed columns | Remediate, Accept as new desired state |
| Chargeback | `usage_daily` grouped by month and cost center; GL code from `cost_center` | Table with totals, export | Export CSV (native list export) |
| Sync health | `sync_run`; `change_log`; system properties for kill switch and token expiry | Tiles, jobs table, change log, kill switch | Toggle kill switch |

## Rules the screens must enforce

- **Plan before submit.** Blocked plans show the rule that blocks them (PRD section 6) and disable Submit. The prototype shows R5, a move without both managers' approval.
- **Policy checks on credit requests.** Show pass, blocked and extra-step states live as the amount changes: cap per role, total overrides per cost center, second approver above a threshold (B5). Disable Submit when anything is blocked. The same checks run again on the server, and the server decides.
- **Kill switch.** When on, show a banner on every screen, keep reads and plans working, and disable Remediate, Submit and Approve actions that would write.
- **Status never by colour alone.** Every meter and chip also carries text (for example "Near limit"), because the PRD alerts at three thresholds.
- **Notice text.** The request form tells users that ServiceNow is the intended path and GitHub's own request button is not tracked here. Blocked-user notifications repeat this.
- **Actor.** Show the audit-log actor on a drift record when GitHub provides one, otherwise "not available" (PRD X2).
- **Empty and error states.** Every table needs a "nothing matches" row. A failed write shows the `change_log` HTTP status and message from `GHCCClient`, not a generic error.

## Catalog item: Request more credits

| Variable | Type | Notes |
| --- | --- | --- |
| User | Reference to `user_map` | Defaults to the requester |
| Amount (USD) | Currency | Must be greater than zero |
| Reason | Single line text | Mandatory |
| Expires | Date | Mandatory; the nightly job reverts the override on this date unless Phase 0 shows GitHub can expire budgets itself |

A catalog client script shows the policy checks as the amount changes. A flow routes approval to the cost center manager, adds the finance approver above the threshold, and on approval calls `GHCCClient.createBudget` or `updateBudget` with the request number.

## Data model gaps found while designing these screens

These are missing from PRD v0.2 section 5 and need adding before the screens can be built as shown:

1. **`request.type` has no value for a bulk repo assignment.** D7 needs one request with one approval for many repos. Add an `assign` type, or reuse `create` and say so.
2. **`drift` has no `actor` field,** though X2 shows the actor on a drift record. Add it, with "not available" as a valid value.
3. **`sync_run` has no call count or rate-limit remaining.** The Sync health tiles need both. Add `calls` and `rate_limit_remaining` (the second depends on Phase 0 showing that GitHub returns rate-limit headers on these endpoints).
4. **The 75, 90 and 100 percent thresholds** are in `policy` as "thresholds", but the prototype also needs the override caps, the second-approver threshold and the total-overrides cap there as separate fields.
5. **The sample forecast** is a run rate (month-to-date spend divided by days elapsed, times days in the month). The PRD says "forecast" without defining it; confirm with FCC finance.

## Not covered here

- The `New cost center` form (the prototype only shows the button), the move-request form, and the create-organization form (F6).
- Mobile layout beyond what the prototype shows: tables scroll sideways inside their panels. UI Builder's responsive behaviour needs checking on a real instance.
- Anything that depends on Phase 0 findings, such as showing a native expiry date on overrides.
