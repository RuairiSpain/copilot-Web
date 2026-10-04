# Overlap fragment: CFG and ENV

Phase 0 research, verified 2026-10-04 against the local clones in `refs/` (Learn source repositories and GitHub schemas, because
learn.microsoft.com is blocked). Test scenario convention used in the catalogue files: `positive` = the rule reports a violation,
`negative` = compliant input with no finding, `skipped` = the rule cannot run and says why.

Versions the schema facts were read against: azd schema `schemas/v1.0/azure.yaml.json`; extensions in the clone azure.ai.agents
1.0.0-beta.18, azure.ai.projects 1.0.0-beta.13, azure.ai.connections 1.0.0-beta.9, azure.ai.toolboxes 1.0.0-beta.9,
azure.ai.routines 1.0.0-beta.8. Learn minimums: azd 1.27.1, azure.ai.agents 1.0.0-beta.8, azure.ai.projects 1.0.0-beta.4.

## Rule table

| ID | Decision | Coverage | Mapped external IDs | Status |
|---|---|---|---|---|
| FND-CFG-001 | adapt | partial | azd extension check `local.azure-yaml` (parseable only, azd ai agent doctor) | verified |
| FND-CFG-002 | native | none | none | verified |
| FND-CFG-003 | native | partial | none (azd/extension fails on some unresolved uses at deploy time) | verified |
| FND-CFG-004 | native | none | none | verified |
| FND-CFG-005 | adapt | partial | PSRule Azure.Deployment.SecureParameter, SecureValue, SecretLeak, OutputSecretValue; Bicep secure-secrets-in-params, secure-parameter-default, outputs-should-not-contain-secrets, use-secure-value-for-secure-inputs; Checkov CKV_AZURE_131, CKV_SECRET_6 | verified |
| FND-CFG-006 | native | partial | none | verified |
| FND-CFG-007 | native | none | none (PSRule HTTPS rules are resource-specific) | product-opinion |
| FND-CFG-008 | native | none | none | verified |
| FND-CFG-009 | native | none | none | verified |
| FND-CFG-010 | native | none | none | product-opinion |
| FND-CFG-011 | native | none | none | verified |
| FND-CFG-012 | adapt | partial | PSRule Azure.Resource.AllowedRegions, Azure.Template.ResourceLocation, Azure.Template.UseLocationParameter; Policy e56962a6-4747-49cd-b67b-bf8b01975c4c, e765b5de-1225-4ba3-bd56-1ac6695af988; Bicep no-hardcoded-location, explicit-values-for-loc-params | product-opinion |
| FND-ENV-001 | native | none | none | verified |
| FND-ENV-002 | native | none | none | product-opinion |
| FND-ENV-003 | native | none | none | product-opinion |
| FND-ENV-004 | native | none | none | product-opinion |

Counts: 16 rules. Status: verified 10 (CFG-001 to 006, 008, 009, 011; ENV-001), product-opinion 6 (CFG-007, 010, 012; ENV-002 to 004), proposed 0, dropped 0.
Decisions: native 13, adapt 3 (CFG-001, 005, 012), reuse 0, wrap 0, drop 0.

Note: no Defender for Cloud or Advisor recommendation maps to any rule in these two groups. None was found for azure.yaml content, and the
Defender/Advisor catalogues were not searched beyond the azure-docs clone.

## Rules set to product-opinion (no platform source)

- FND-CFG-007: no source states that http targets or loopback/metadata destinations are rejected. Only the "remote MCP endpoint" requirement is sourced.
- FND-CFG-010: property facts (versionUpgradeOption semantics, required model.version) are sourced; the decision to require pinning per profile is opinion.
- FND-CFG-012: the subscription location list API is sourced; no source read says a malformed or unlisted location fails deployment.
- FND-ENV-002, FND-ENV-003, FND-ENV-004: Foundry Doctor cross-environment correlation; azd only documents environment isolation as intent.

No rule was changed to `dropped`.

## Unverified

- Duplicate YAML keys (CFG-001): how azd core treats them is unverified (azd core source not in the clone). Detection is a Foundry Doctor addition.
- CFG-003: whether `uses` may name a Bicep-defined resource for azure.ai.* hosts; generic schema says "service names and resource names", Foundry docs say service names. Routine-must-use-agent comes from the schema description only.
- CFG-004: whether `project` is required for a hosted agent that sets `image` (schema requires it; docs example suggests image skips the Dockerfile build). The schema's "project and image together are invalid" statement is on the generic service and enforced for host containerapp only.
- CFG-005: whether each authType carries a secret in `credentials`; CKV_SECRET_6 semantics (README example only). Docs conflict: Foundry reference keeps secrets in the azd environment via `${VAR}`, azd docs say never keep secrets in `.env` and recommend `azd env set-secret`.
- CFG-006: that all Bicep outputs are written to the azd environment (azd core not in the clone); `${VAR:-default}` verified from extension source only; AGENT_<SERVICE> name normalisation rule.
- CFG-008: spelling and acceptance of `allowed_tools` inside azure.yaml tool entries (documented for REST/SDK MCP tools; no azure.yaml example found).
- CFG-009: preview inconsistencies in routine schedule keys (`cron` vs `cron_expression`; trigger type `schedule` vs `recurring`) and whether `time_zone` is required. Five-field and five-minute facts are verified.
- CFG-010: API version in which `versionUpgradeOption` is available; whether azure.ai.project can set it (schema has no field). Not found in the rest-api-specs search done.
- CFG-011: per-construct introduction/retirement extension versions (no per-field changelog read).
- CFG-012: which location form azd stores (docs show `eastus`); region availability of Foundry models is not covered.
- ENV-001: precedence of AZURE_RESOURCE_GROUP versus azure.yaml `resourceGroup` (azd core not in the clone).
- Bicep linter documentation: `refs/bicep/docs` has no linter rule pages; rule codes were read from `refs/azure-docs/articles/azure-resource-manager/bicep/linter-rule-*.md`.
- Defender for Cloud and Advisor: not searched in depth; mapping arrays are empty rather than guessed.

## PRD / process ambiguities

- Implementation owner for `adapt`: the brief says "native unless reuse/wrap/adapt"; the schema allows `adapter`. CFG-001, CFG-005 and CFG-012 use `adapter`. If the intent is that adapt means native code that borrows the idea, change them to `native`.
- Platform basis forces `error` in all profiles; CFG-001, CFG-003, CFG-004 and CFG-009 use it. CFG-011 and CFG-006 are `operations` because the sources describe deprecation or empty values, not a platform failure.

## Proposed new rules (fragment only, no catalogue files)

CFG
1. Hosted-agent runtime contract completeness (deployment feasibility): hosted agent declares protocols and a startup path consistent with the runtime contract and ports; deploy mode (code vs container) matches the files present. Sources to read: `hosted-agent-contract.md`.
2. requiredVersions drift (schema lifecycle): azure.yaml `requiredVersions` is missing, or lower than the extension versions actually installed or used by constructs in the file; reports azd 1.32.0 for voice.
3. Existing-project deployment drift (release readiness): `azure.ai.project` with `endpoint` set while `deployments` are also declared (azd manages them), and memory-store/agent model names that cannot be resolved offline, as a preflight list for the control-plane check.

ENV
1. Environment completeness for promotion (release readiness): each environment has the keys that the others use for required variables referenced in azure.yaml, so promotion does not fail with an unresolved variable.
2. Model deployment capacity parity (cost visibility): sku.capacity and sku.name differences between environments summarised with TPM totals, flagging prod below test.
3. Environment secret hygiene (supportability): `.azure/` ignored by git and no secret-looking keys in tracked `.env`; complements CFG-005 for the environment half.
