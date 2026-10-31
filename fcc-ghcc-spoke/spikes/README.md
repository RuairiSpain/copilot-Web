# Phase 0 spikes

One script per **Verify** item in the PRD, each run against a throwaway GitHub enterprise with a classic token that has `manage_billing:enterprise`. Never point a spike at FCC's enterprise.

Each script should:

1. Read `GITHUB_TOKEN` and `GITHUB_ENTERPRISE` from the environment.
2. Make the calls described in the spike table of the implementation plan.
3. Write raw requests and responses (token removed) to `findings/<spike-name>.json`.
4. Print a one-line result: accepted, rejected, or inconclusive.

| Spike | Gates |
| --- | --- |
| teams-as-resources | Phase 2 |
| org-as-resource | Phase 2 |
| property-to-assignment | Phase 5 |
| org-vs-repo-precedence | Phase 5 |
| budget-overlap-precedence | Phase 3 |
| override-expiry-field | Phase 4 |
| user-state-response | Phase 4 |
| azure-subscription-api | Phase 2 |
| rate-limits | Phase 3 |
| audit-log-actor | Phase 3 |
| github-app-auth | Risk mitigation |

Findings files are committed (with the token and any real user names removed) because the mock server is built from them.
