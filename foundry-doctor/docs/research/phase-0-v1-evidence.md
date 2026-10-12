# Phase 0 V1 evidence refresh

Date: 2026-10-06

This note records the Phase 0 closure work that was safe to do in-tree without
changing rule logic. It focuses on V1-facing claims: current source verification,
safe overlap de-provisionalisation where tool scope is clearly inapplicable, and
explicit blocked items that still need external evidence.

## Current-source verification performed

The following source URLs were re-read against current published documentation in
this closure pass using Microsoft Learn fetches or public web pages. The rule
catalogue keeps `lastVerified` at `2026-10-05`, which is the latest non-future
date accepted by the in-tree catalogue invariants.

| Area | URL | Result |
|---|---|---|
| azd environments | `https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview` | Verified current page still states each environment has its own resource group and stores `.azure/<env>/.env` values. |
| azd pipeline setup | `https://learn.microsoft.com/azure/developer/azure-developer-cli/configure-devops-pipeline` | Verified current page still documents `azd pipeline config` and `azure-dev.yml` in GitHub Actions / Azure Pipelines locations. |
| azd environment variables | `https://learn.microsoft.com/azure/developer/azure-developer-cli/manage-environment-variables` | Verified current page still documents `.azure/<env>/.env`, Bicep output capture, and environment-variable handling. |
| azd environment secrets | `https://learn.microsoft.com/azure/developer/azure-developer-cli/environment-secrets` | Verified current page still documents `azd env set-secret` and Key Vault-backed environment references. |
| Foundry `azure.yaml` reference | `https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference` | Verified the current page still describes preview `azure.yaml` hosted-agent/project shapes and schema-backed validation. |
| Bicep output secret rule | `https://learn.microsoft.com/azure/azure-resource-manager/bicep/linter-rule-outputs-should-not-contain-secrets` | Verified the current page still treats secure params, `list*()` results, and password-like output names as violations. |
| Bicep linter overview | `https://learn.microsoft.com/azure/azure-resource-manager/bicep/linter` | Verified the current page still lists linter rule configuration and rule-table behavior. |

## Metadata changes made

### Verification-date refresh

The following catalogue entries had their primary-source `lastVerified` dates
confirmed again in this pass while keeping `lastVerified: 2026-10-05` so the
catalogue stays within the current in-tree date invariant:

- `FND-CFG-001`
- `FND-CFG-004`
- `FND-CFG-005`
- `FND-ENV-001`
- `FND-ENV-002`
- `FND-ENV-003`
- `FND-ENV-004`
- `FND-OPS-007`
- `FND-SEC-014`

### Honest confidence downgrades

Two V1 rules still include explicit unverified caveats in their own notes, so
their evidence confidence was reduced from `certain` to `likely`:

| Rule | Why the downgrade was needed |
|---|---|
| `FND-CFG-001` | Duplicate-key behavior is a Foundry Doctor addition; the current azd core duplicate-key handling was not verified from a primary source. |
| `FND-CFG-004` | The catalogue still records an unverified edge case about whether `project` remains required when `image` is set. |

## Overlap decisions de-provisionalised safely

The ENV rules are source/configuration correlation checks over sibling azd
environments. They do not inspect a single Azure resource or a single ARM/Bicep
template in isolation. For these rules, the configured overlap tools are
inapplicable by scope, so their research state was completed as `searched-none`
and the provisional flag was removed:

- `FND-ENV-001`
- `FND-ENV-002`
- `FND-ENV-003`
- `FND-ENV-004`

Why this was safe:

- PSRule for Azure, Azure Policy, Defender for Cloud, and Azure Advisor operate
  on deployed resources, templates, or service recommendations, not sibling azd
  environment comparisons.
- The Bicep linter operates on one Bicep template, not `.azure/<env>/.env`
  trees or per-environment substitution.
- Checkov and KICS both scan IaC or CI/CD files, but this rule family is about
  cross-environment comparison semantics, not a misconfiguration within one
  template or workflow file.

## Third-party overlap evidence consulted

Published tool documentation re-read during this refresh:

| Tool | URL | Evidence used |
|---|---|---|
| PSRule for Azure | `https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Storage.LocalAuth/` | Confirms rule-document format and stable rule-ID style used by the overlap metadata. |
| Checkov | `https://www.checkov.io/2.Basics/CLI%20Command%20Reference.html` | Confirms CLI and framework-driven scanning model; used as supporting evidence for adapter/documentation scope, not as compatibility proof. |
| KICS commands | `https://docs.kics.io/latest/commands/` | Confirms CLI contract and report-format model. |
| KICS platforms | `https://docs.kics.io/latest/platforms/` | Confirms ARM/Bicep and GitHub-workflow scanning scope. |

## Still blocked

### XF provenance

The PRD's 59 XF-origin checks remain blocked. No authoritative source asset
bundle, stable XF identifiers, source revision, or rule mapping was supplied in
the repository. See `docs/xf-origin-inventory.md`.

### Adapter compatibility claims

This refresh improves provenance hygiene, but it does **not** prove runtime
compatibility for PSRule, Checkov, or KICS adapters. There is still no in-tree
adapter parser contract or pinned structured-output fixture set for those tools.

### Final Phase 0 closure

This work does not manufacture the evidence that PRD section 22 still requires:

- a successful strict gate on a supported host,
- a successful current Linux CI run,
- successful pinned spike evidence for the azd isolation path,
- and release-artefact licence / NOTICE evidence.
