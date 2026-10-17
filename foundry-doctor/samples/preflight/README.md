# Preflight samples

Samples for `.github/workflows/foundry-doctor-preflight.yml` (reusable workflow, OIDC, protected environment).

- `reusable-caller.example.yml`: caller workflow, including the optional `what-if` input (default `false`).
- `github-oidc.example.yml`: OIDC federated credential sample.
- `preflight-role.json`: read-only custom role for the default run (includes `Microsoft.CognitiveServices/accounts/deployments/read`, used by FND-DEP-005).
- `preflight-whatif-role.json`: optional addendum for `what-if: true`, resource-group scope.
- `preflight-whatif-subscription-role.json`: same addendum, subscription scope.

## What-if rights

`what-if: true` adds `--what-if`. ARM what-if needs the same rights as deploying the template, so it is not read-only.
The right must be held at the scope the template deploys to:

- Resource-group-scope template: assign `preflight-whatif-role.json` at the target resource group.
- Subscription-scope template (`targetScope = 'subscription'`): the what-if right is needed at the subscription, so assign
  `preflight-whatif-subscription-role.json` at the subscription. This is broader; prefer resource-group scope when possible.

In both cases add the write actions of the resource types in your template, and assign the addendum together with
`preflight-role.json`. Leave `what-if` at `false` for identities that should stay read-only.

The workflow does not use `--strict` because some checks (FND-DEP-008 Incremental mode, FND-DEP-009) are uncertain by design,
so `--strict` would always exit 3. See the foundry-doctor README, "Exit codes".
