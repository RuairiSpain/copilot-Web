# Expected results for the fixtures

These expectations were derived by reading the rules. They have **not** yet been confirmed by a PSRule run: PSRule
was not available when they were written (see `../README.md`, Status). `Invoke-FixtureCheck.ps1` in this folder asserts
them, and the workflow `.github/workflows/foundry-rules.yml` runs it; until its first run the result is unknown. The
bodies of the 8 PowerShell rules were additionally executed outside PSRule and matched these expectations for the
resources they apply to.

The ARM JSON fixtures are hand-written resource objects in the shape PSRule for Azure produces after expansion
(one object per resource, with `type`, `name`, `kind`, `sku`, `identity`, `properties`). They were not produced by
compiling Bicep.

Automated: `pwsh -NoProfile -File tests/Invoke-FixtureCheck.ps1` from the `foundry-rules` folder (exit 1 on any failed
assertion). By hand, one fixture at a time, with the prod baseline:

```powershell
Invoke-PSRule -InputPath tests/azure.bad.yaml -Baseline Foundry.Prod -Module PSRule.Rules.Azure -Outcome Fail
```

"Must fail" lists rules that must report Fail for the named resource. Other Foundry rules may also fail on a fixture;
only the listed ones are asserted. "Must not fail" means no Foundry rule with status `evaluated-subset` reports Fail.
Rules with status `not-evaluated` must report no result at all (neither Pass nor Fail) on every fixture.

## azure.good.yaml

Must not fail: FND-CFG-002, FND-CFG-003, FND-CFG-008, FND-CFG-011.

## azure.edge.yaml

Service names, project paths and references are `${VAR}` expressions. Must not fail: FND-CFG-002, FND-CFG-003.
(The catalogue reports these as uncertain; this package leaves them out of the result.)

## azure.bad.yaml

| Rule | Why |
|---|---|
| FND-CFG-002 | `triage` and `triage-copy` have the same project `src/triage` and name `triage-agent` |
| FND-CFG-003 | `triage` lists toolbox `agent-tools` but does not have it in `uses`; the toolbox tool names connection `missing-conn`, which is not a service |
| FND-CFG-008 | the mcp tool has `allowed_tools: []` |
| FND-CFG-011 | `legacy` uses host `microsoft.foundry`; `triage` has `config` and an `FOUNDRY_ENDPOINT` env key |

## arm.good.json

Resources: Foundry account, project, deployment, Search, Storage, Cosmos DB, Key Vault, private endpoint,
role assignment, federated credential.
Must fail: FND-COST-002 only (Search `replicaCount` 3 is flagged by the dev cost rule; at prod severity it is Information).
Must not fail: every other evaluated rule, including FND-OPS-004 (no required tags configured, so it is skipped).

## arm.bad.foundry-search.json

| Resource | Must fail |
|---|---|
| account `foundrybad01` | FND-SEC-001, FND-NET-001, FND-IDN-001, FND-SEC-010, FND-SEC-011 |
| project `foundrybad01/proj1` | FND-IDN-001 |
| Search `search-bad` | FND-SEC-002, FND-REL-001, FND-SEC-010 |
| Search `search-free-bad` | FND-IQ-012, FND-SEC-002 |

## arm.bad.storage-cosmos-kv.json

| Resource | Must fail |
|---|---|
| Storage `stfoundrybad01` | FND-SEC-004, FND-REL-002, FND-SEC-006 (0.0.0.0/0 and 10.1.0.0/16), FND-SEC-010 |
| Cosmos DB `cosmos-foundry-bad` | FND-SEC-003, FND-REL-003, FND-SEC-006 (0.0.0.0 and 100.64.0.0/10), FND-SEC-010 |
| Key Vault `kv-foundry-bad` | FND-SEC-005 (purge protection false, RBAC authorization absent) |

## arm.bad.deployments-apim.json

| Resource | Must fail |
|---|---|
| deployment `foundrybad01/chat-ptu` | FND-REL-008, FND-COST-002, FND-CFG-010 |
| API Management `apim-foundry-bad` (Basic, External) | FND-NET-009 |
| API Management `apim-foundry-dev` (Premium, capacity 2) | FND-COST-002 |
| backend `apim-foundry-bad/foundry-backend` | FND-REL-006 |

`apim-foundry-dev` must not fail FND-NET-009 (no virtualNetworkType, so the rule does not apply).

## arm.bad.identity.json

| Resource | Must fail |
|---|---|
| role assignment `3333...` (no principalType) | FND-IDN-003 |
| federated credential `deploy-id/github-bad` | FND-IDN-006 (http issuer, wildcard subject) |
| account `foundrybad02` (identity type None) | FND-IDN-001, FND-SEC-001, FND-NET-001 |

## arm.bad.names.json

Must fail FND-OPS-005 for each of: account `x`, Storage `My-Storage`, Key Vault `kv--bad`, Search `Search_Bad`,
Cosmos DB `Cosmos_Bad`, Container Registry `ab`, Log Analytics workspace `a`.

## arm.bad.search-sizing.json

Must fail FND-REL-005 for each of: `search-size-a` (partitionCount 5), `search-size-b` (basic with 4 replicas),
`search-size-c` (12 replicas x 4 partitions = 48 > 36), `search-size-d` (HighDensity on standard).

## Not-evaluated rules

On every fixture, the 80 rules with status `not-evaluated` (see `../docs/not-evaluated.md`) must produce no Pass and
no Fail. If one appears as Pass, the `Foundry.NotEvaluated` type guard is broken.
