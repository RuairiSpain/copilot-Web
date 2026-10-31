# Phase 0 spikes

One script per **Verify** item in the PRD. Each creates short-lived objects named `spike-<name>-<run id>`, records every request and response to `findings/<spike>.json` (token never recorded), prints a one-line verdict, and deletes what it created.

**Run these only against a throwaway enterprise**, with a classic token that has `manage_billing:enterprise` (the property spike also needs organization admin rights). The scripts refuse to run against `api.github.com` unless `SPIKE_CONFIRM_THROWAWAY=yes` is set.

## Running

```sh
export GITHUB_TOKEN=...            # classic token, throwaway enterprise
export GITHUB_ENTERPRISE=my-test-enterprise
export SPIKE_CONFIRM_THROWAWAY=yes

SPIKE_TEAM=platform                      node spikes/teams-as-resources.js
SPIKE_ORG=my-org SPIKE_REPO=my-repo      node spikes/property-to-assignment.js
SPIKE_USER=some-login                    node spikes/budget-overlap.js
```

The other eight follow the same pattern; each file's header comment lists its inputs and what its verdicts mean.

To rehearse against the mock instead, start `npm run mock` and set `GITHUB_API_BASE=http://127.0.0.1:4010`, `GITHUB_TOKEN=test-token`. No confirmation is needed. The mock's answers are guesses, so a rehearsal checks the script, not GitHub.

## Status

| Spike | Script | Gates | Inputs |
| --- | --- | --- | --- |
| teams-as-resources | written | Phase 2 | `SPIKE_TEAM` |
| property-to-assignment | written | Phase 5 | `SPIKE_ORG`, `SPIKE_REPO`, optional `SPIKE_POLL_SECONDS` |
| budget-overlap | written, precedence needs a manual step | Phase 3 | `SPIKE_USER`, optional `SPIKE_KEEP=yes` |
| org-as-resource | written, billing attribution needs a manual step | Phase 2 | `SPIKE_ORG`, `SPIKE_USER` |
| org-vs-repo-precedence | written, billing attribution needs a manual step | Phase 5 | `SPIKE_ORG`, `SPIKE_REPO` |
| override-expiry-field | written | Phase 4 | `SPIKE_USER` |
| user-state-response | written | Phase 4 | `SPIKE_USER` |
| azure-subscription-api | written | Phase 2 | optional `SPIKE_AZURE_SUBSCRIPTION` (without it a rejection is inconclusive) |
| rate-limits | written, read-only | Phase 3 | optional `SPIKE_RATE_CALLS` |
| audit-log-actor | written; also rerun after a UI edit | Phase 3 | optional `SPIKE_AUDIT_ONLY`, `SPIKE_AUDIT_WAIT_SECONDS` |
| github-app-auth | written | Risk mitigation | `SPIKE_APP_TOKEN` (an installation token you minted) |

## Reading a finding

Each file has `verdict` (`accepted`, `rejected` or `inconclusive`), a plain-language `summary`, `details` with the facts the spike was after, and `steps` with every request and response. Commit findings after checking that no real user names or emails need removing; the mock server and `lib/endpoints.js` are corrected from them.

The budget spike cannot decide which overlapping budget wins, because that needs real usage. Run it with `SPIKE_KEEP=yes`, follow the manual step in its finding, then delete the budgets and cost center by hand.
