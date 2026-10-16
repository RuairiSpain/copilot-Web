# Phase 1 final verification: Azure black belt (head 36acd92, code identical at 47d5aa7)

Reviewer: `azure-black-belt` (independent). Date: 2026-10-16. Verdict: **ACCEPT with conditions**. No blocker. Acceptance condition: fix P1 (or narrow the catalogue wording), and fix P2 and P3 or record them as Phase 1 limitations in ADR-018.

Method: Go 1.26.9 binary, `--azd-version 1.35.0`, Bicep CLI 0.48.1, no Azure credentials. All 87 earlier projects (r/, r2/, v3/) with the round-2 and the new binary, Microsoft samples 15 and 41 (sample 15 with variants), and about 15 new projects. Microsoft Learn was unreachable; Azure facts come from search results that quote Learn.

## Status of the round-2 findings

| id | status |
|---|---|
| R1 substitution caps | fixed (16 nested modules, each passing `${a}` eight times, under `ulimit -v 6000000`: exit 0, no out-of-memory; 29 s is the 16 module-file compiles, bounded at 64 files and 3 min) |
| R2 scope tokens, module path | fixed |
| R3 module outputs | fixed (SEC-002 evaluates the Search service behind the connection; the old false positive is gone) |
| R4 Owner at resource scope | fixed (IDN-004 error) |
| R5 `Location.File` | fixed (`infra/modules/search.bicep`; a 16-level chain gives `infra/modules/m15.bicep`) |
| R6 CFG-010 without Bicep | fixed (`input-unavailable: compiled ARM model unavailable`; prod exits 2) |
| R7 module `dependsOn` | fixed (graph shows the edge) |
| NET-001 severities | verified: forbidden = info/warning/error (dev/test/prod); entra-only = info/warning/warning; allowed = info/info/info; the override only lowers, dev stays info |
| Parameter files (ADR-018) | works with limits (P2, P3, P7) |
| Default-derived confidence | works, broader than needed (P4) |
| OPS-004 | evaluates (tags expression versus absent) |
| IDN-001 | not re-tested (AIServices-only) |

Parameter override checks that worked: direct `if (deploySearch)` and `publicNetworkAccess: publicNetworkAccess` resolve from the file; wrong types and `[concat(...)]` strings are not injected; a string value `x') , parameters('publicNetworkAccess` is quoted as a literal; a secure parameter value never appears in the report; `${NOPE=dflt}` resolves; a 1.1 MB file gives `parameter-file-too-large`; 2500 parameters gives `parameter-file-too-many-parameters`.

Sample 15 with a parameter file: 0 additional rules evaluate (97 skipped with and without). Its conditions live in module-local variables (`useExistingAccount = !empty(existingAccountResourceId)`) and root variables (`searchPassedIn`); injection replaces only root `parameters('x')`.

## Findings

| id | sev | where | issue | fix |
|---|---|---|---|---|
| P1 | major | `internal/rules/idn/idn004.go:291-299`, `FND-IDN-004.yaml` | The info downgrade for Contributor at "resource scope" applies to any single resource, but the catalogue justification covers only a single Foundry resource. Contributor on a Storage account (includes `listkeys/action`) and on a Key Vault under the access-policy model can reach data. Run: Contributor for the account identity on Storage and Key Vault each gave `info`. | Restrict info to `Microsoft.CognitiveServices/accounts`, `accounts/projects` and Foundry-related resources; keep Contributor on Storage, Key Vault, Cosmos DB and Search at the normal severity. |
| P1b | minor | same | Contributor on a Foundry account with `disableLocalAuth` false can list keys; the info wording hides that. | Keep info only when local auth is provably disabled, otherwise warning. |
| P1c | minor (assumption) | `FND-IDN-004.yaml` notes | "Foundry sources list Contributor as the minimum role for several tools" and the CI/CD quickstart claim could not be re-fetched. | Re-verify the cited rbac-foundry and set-up-cicd-hosted-agent pages. |
| P2 | major | `internal/app/params.go` (BuildParams), `internal/bicep/params.go` | A hostile `.bicepparam` can read files outside the project and put them into the report: `param accountName = loadTextContent('../../outside.txt')` put the file content into the JSON six times, including as the resource name of the NET-001 finding. This breaks ADR-018 point 7. `main.bicep` has the same exposure (not tested). | Reject entry, module and parameter files that call `loadTextContent`, `loadFileAsBase64`, `loadJsonContent`, `loadYamlContent` or `loadDirectoryFileInfo` with a path outside the project root (or a non-literal path), or sandbox the compile working directory; add a warning and a test. |
| P3 | major (limitation, not a wrong result) | `internal/armmodel/params.go` `injector`, `resolveString` | Variable-mediated conditions are not resolved, so a parameter file does not help the commonest template shape; the brief and ADR-018 consequence 1 are overstated. | Evaluate root and child variables inside conditions, names and property values using the existing `Variable` machinery, or record the limitation in ADR-018 and the docs. |
| P4 | minor | `internal/rules/rules.go:325-346` `capDefaultDerived` | The cap fires on any defaulted parameter of the resource, including a name or location that does not affect the finding; evidence never says "from the default of parameter X". | Cap only when a parameter feeding the checked property or condition is defaulted, or name the parameters in the evidence. |
| P5 | minor | `internal/rules/net/rules.go:75-90` | `entra-only` lowers severity without checking `disableLocalAuth`. | Skip the downgrade, or add a note, when local auth is not provably disabled. |
| P6 | minor | `internal/rules/idn/idn.go:365-369` | IDN-003 name-only matches keep severity `error` in prod (confidence medium, evidence "likely"); `--fail-on error` still fails on a heuristic. | Lower the severity of a name-only finding to warning. |
| P7 | minor | `internal/app/params.go` | Values ignored for type mismatch produce no warning. | Name the ignored parameters in one warning. |
| P8 | minor (unverified, carried over) | `FND-NET-001.yaml` fix and apiVersions; OPS-003 fix | `Microsoft.CognitiveServices/accounts@2026-09-01` (marked stable) and `Microsoft.Insights/metricAlerts@2026-01-01` could not be confirmed (newest CognitiveServices page found 2026-05-15-preview; metricAlerts only 2018-03-01). | Confirm against the Learn template reference or use confirmed versions; do not ship unverified API versions in fix snippets. |

## Unsupported assumptions

The 2026-09-01 and 2026-01-01 API versions (P8); the Foundry-sources claim behind the IDN-004 info decision (P1c); that parameter files make module-internal conditions resolvable (they do not, P3).

## Missing tests

Contributor at Storage and Key Vault scope (P1); `.bicepparam` with `loadTextContent` outside the project (P2); a variable-mediated condition with a parameter file (P3); wrong-type and `[` parameter values producing a warning (P7); entra-only with local auth enabled (P5).

## Catalogue wording check

The IDN-004 info finding ("Contributor granted to an application identity at resource scope ...; informational only, a narrower Foundry role is preferred") matches the catalogue sentence for a single Foundry resource and conflicts with the catalogue for non-Foundry resources (P1). It is consistent with Microsoft's privileged administrator roles list (Owner, Contributor, Reservations Administrator, RBAC Administrator, User Access Administrator), which could not be re-fetched.
