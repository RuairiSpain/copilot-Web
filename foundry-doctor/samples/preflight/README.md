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

The workflow does not use `--strict` because some checks (FND-DEP-002, FND-DEP-008 Incremental mode, FND-DEP-009) are uncertain by design,
so `--strict` would always exit 3, and exit 3 outranks exit 1 (ADR-010 precedence 4,2,3,1,0), so it would also mask a real error finding.
Exit 0 does not mean ready: read the report, which prints `N check(s) uncertain`. See the foundry-doctor README, "Exit codes".

## Pinned Bicep digests: how to re-derive them

The workflow installs Bicep 0.48.1 and verifies a SHA256 per platform asset. The Azure/bicep release publishes no checksum file, so the values
were computed from the release assets on first download (2026-10-17). When bumping Bicep (and `ContractVersion` in the Go code), recompute all
four values from the new release, on a trusted machine, and update `v` and the four `sha=` values together in the workflow:

```sh
v=<new-version>
for a in bicep-linux-x64 bicep-linux-arm64 bicep-win-x64.exe bicep-win-arm64.exe; do
  curl -fsSL "https://github.com/Azure/bicep/releases/download/v$v/$a" | sha256sum | sed "s/-/$a/"
done
```

Download each asset twice (or from two networks) and compare, because the digest is trust on first download. Do not copy a digest from a third-party page.

## Using the reusable workflow from another repository

Pin `uses: ...@<ref>` and `source-ref` to a full 40-hex commit SHA rather than a tag: a tag can be re-pointed after review, a SHA cannot.
