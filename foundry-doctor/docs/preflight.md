# Azure preflight (`foundry-doctor preflight`)

`preflight` runs the `FND-DEP-001`..`FND-DEP-012` rules against live Azure
state. It only reads: GET requests plus the sanctioned POST reads (name
checks, policy restriction checks and, with opt-in, ARM what-if). Nothing is
created, changed or deleted.

Readiness is **advisory**. A `ready` result never guarantees a deployment will
succeed. The report ends with a summary of `ready`, `blocked`, `uncertain` and
`skipped` checks.

Readiness is computed from the raw rule outcomes, before `--min-severity`,
baseline and suppression filtering. Any finding from a rule (at any severity,
baselined or suppressed) marks that rule `blocked`; a skipped or uncertain rule
is never `ready`. Hiding a finding from the report therefore cannot make a
failing check look ready.

## Rule evidence notes

- **FND-DEP-003** reads the Microsoft.Authorization permissions API for the
  resource group and checks `<provider>/<type>/write`, `deployments/write`,
  `roleAssignments/write` when the template assigns roles, and
  `<provider>/register/action` for template namespaces that are not
  `Registered`. A grant is evidence, never proof (deny assignments, conditions,
  locks, policy and PIM are not reflected), so a fully granted set is `uncertain`.
- **FND-DEP-010** fails on ReadOnly locks, on CanNotDelete locks covering a
  predicted delete (needs `--what-if`), and on a CanNotDelete lock on the
  resource group when the deployment history is within 80 of the documented
  limit of 800 (the 80 margin is a Foundry Doctor product decision).
- **FND-DEP-011** fails when the agent subnet carries a service association
  link or resource navigation link owned by another resource (reported as
  likely, not certain).
- **FND-DEP-012** checks the target location against the subscription's
  locations and every template resource type against the provider's
  locations (both certain), then compares only the documented Foundry Agent
  Service columns this implementation can map deterministically (Responses API,
  Agents, private VNet) with a dated copy of the Learn supported-regions table
  (2026-09-07). Tool-by-region and Bing-region claims are intentionally out of
  scope here. A mismatch with the supported-regions table is only ever
  `uncertain`; once the copy is over a year old it is skipped instead.

## Outcomes

| Outcome | Meaning |
|---|---|
| pass | The check ran and found no problem. |
| fail (blocked) | A finding. Names the resource ID or setting. |
| skipped | The check could not run. The reason names the exact capability and the missing permission. Never counted as a pass. `--strict` exits 3. |
| uncertain | The check ran but cannot prove the condition (for example FND-DEP-009 policy effects, FND-DEP-003 permission evidence). Never counted as a pass. |

## Flags

| Flag | Purpose |
|---|---|
| `--subscription`, `--tenant`, `--location`, `--resource-group` | Target. Default to `AZURE_SUBSCRIPTION_ID`, `AZURE_TENANT_ID`, `AZURE_LOCATION`, `AZURE_RESOURCE_GROUP`. |
| `--what-if` (alias `--preflight`) | Opt in to ARM what-if (FND-DEP-008). Off by default. |
| `--approved-scope <id>` | Extra scope that cross-resource-group references may target. Repeatable. |
| `--allow-delete <id>` | Accept a predicted deletion of this resource. Repeatable. |
| `--strict` | Exit 3 when any check was skipped. |

## Cross-resource-group scope policy

A resource a rule inspects (the agent subnet in FND-DEP-011, every what-if
change in FND-DEP-008) must lie under the target resource group or under an
`--approved-scope`. Matching is case-insensitive and on path boundaries
(`.../resourceGroups/oth` does not approve `.../resourceGroups/other`).

| Situation | Result |
|---|---|
| Inside the target resource group | Evaluated normally. |
| Under an approved scope | Evaluated normally. |
| Outside both | Finding naming the resource ID. The resource is not read. |
| Target subscription or resource group not set | Skipped: capability `cross-resource-group scope policy` is named. |

## Least-privilege access

Use a dedicated identity per environment. The actions come from
[permissions-matrix.md](permissions-matrix.md); the matrix marks which
api-versions are verified. Whether built-in **Reader** grants every read is a
hypothesis: tests prove only that a 403 degrades to a skipped check.

- Reader on the subscription covers the GET reads in the matrix (hypothesis),
  including `Microsoft.PolicyInsights/checkPolicyRestrictions/read` (FND-DEP-009;
  catalogue: "Reader suffices"; the call is a POST but the action is a read).
- These POST actions are not read actions. Grant them only if you want those
  checks to run; otherwise they skip and name the action:
  - `Microsoft.CognitiveServices/checkDomainAvailability/action` and
    `Microsoft.Search/checkNameAvailability/action` (FND-DEP-007; the KeyVault,
    Storage and APIM name checks use `.../checkNameAvailability/read`)
  - `Microsoft.Resources/deployments/whatIf/action` (FND-DEP-008, `--what-if` only)

A starter custom role is in
[`samples/preflight/preflight-role.json`](../samples/preflight/preflight-role.json).
Review it against the matrix before assigning. Scope it to the subscription or
the target resource group and keep it free of write actions.

A restricted identity is supported: each missing action skips only the rule
that needs it, and the skip reason names the capability and permission.

## OIDC in GitHub Actions (no secrets)

The workflow `.github/workflows/foundry-doctor-preflight.yml` (manual
`workflow_dispatch`) signs in
with `azure/login` using OIDC. Store only identifiers (no secrets) as
environment variables on a protected GitHub environment. See
[`samples/preflight/github-oidc.example.yml`](../samples/preflight/github-oidc.example.yml)
for a copy-and-adapt sample that builds the CLI and runs the same steps.

Create a federated credential on the app registration whose subject is the
protected environment, for example
`repo:<owner>/<repo>:environment:<environment-name>`. Do not create a client
secret.
