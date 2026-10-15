# Phase 1 verification: Foundry lead (head 2c14b36)

Reviewer: `foundry-lead` (independent). Date: 2026-10-15. Verdict: **REJECT**. Most earlier blockers are fixed; the remaining defects are false passes, false fails or hidden findings.

Method: binary built with Go 1.25.13 and run against about 70 generated projects (including `.env` files). Learn was unreachable; platform claims rest on the azd schemas (azure.yaml.json, azure.ai.agent.json), a local copy of the Learn `azure-yaml-reference` page, the releases page and the agents CHANGELOG. Go tests for rules, app, findings, project and azureyaml pass; the whole module was not run by the reviewer.

## Remaining or new findings

| id | sev | where | issue | fix |
|---|---|---|---|---|
| R1 | major (blocking) | `internal/rules/rules.go:309-351`, `internal/findings/findings.go:24-31` | Fingerprint v2 depends on siblings: most rules set no `Finding.Key` (only net, idn, sec do), so a CFG-004 finding's fingerprint changes when a second finding appears on the same service, and a baselined finding resurfaces. Findings with equal evidence but different locations are de-duplicated (duplicate `host` keys give one finding). All CFG-001 findings without a resource share one fingerprint. | Set `Finding.Key` in every rule (variable name for CFG-006, field path for CFG-004/005, check kind for CFG-001). Drop the evidence-hash fallback or make it deterministic per finding. Do not de-duplicate findings with different locations. |
| R2 | major (blocking) | `cfg/cfg005_012.go:24` `nonSecretNameRe` | The unanchored `(limit\|count\|rotation\|size\|file)` exclusion matches inside `ACCOUNT` and `PROFILE`: `STORAGE_ACCOUNT_KEY` and `PROFILE_PASSWORD` with literal values produce no finding and no skip (false pass). | Match `_`-delimited tokens, not substrings. |
| R3 | major (blocking) | `app/providers.go:228-231`, `secretishKey` 176-183 | Values are digested before ENV rules classify them; a key containing KEY, SAS, SIG or TOKEN loses its resource-ID shape, so ENV-003 silently passes a Key Vault shared by prod and non-prod (`AZURE_KEYVAULT_ID`). The same value under `AZURE_VAULT_ID` is reported. | Classify before digesting, or carry a value-shape flag next to the digest. |
| R4 | major | `cfg/cfg005_012.go:230-241` | CFG-006 false fail: `AGENT_<SERVICE>_<PROTOCOL>_ENDPOINT` for `responses`, `invocations`, `invocations_ws` is listed in the Learn reference but not accepted; the loop applies to every service, not only agent services. | Accept those names for agent-host services only; other `AGENT_<SVC>_*` are uncertain. |
| R5 | minor | `ops/repofiles.go:205-211` `reAzdDeploy` | OPS-007 passes on `echo hi # azd up`, `echo "skipping azd up"` with `azd deploy` in dead code, and `echo azd provision`. | Tokenise the shell line and require `azd` in command position; otherwise uncertain. |
| R6 | minor | `rules/skipclass.go:30-55`, `cfg008_010.go:17`, `env/env.go:80` | `--strict` classification mixes not-applicable with missing input: CFG-008/009 and ENV-001..004 (fewer than two environments) use bare `input-unavailable`; OPS-008 `project-has-no-agent-services` is not in `byDesignHeads`; CFG-006/012 with no selected environment use bare `input-unavailable`. `samples/good --strict` exits 3. | Emit `not-applicable:` for "no such resource"; add `project-has-no-agent-services`; state the missing input elsewhere. |
| R7 | minor | `cfg/cfg005_012.go:268-290`, `FND-CFG-012.yaml` | CFG-012 note says display names are warnings, but `AZURE_LOCATION=East US` is an error in prod. No source says ARM rejects it (M2 not fixed). | Uncertain or warning in every profile, or cite a source. |
| R8 | minor | `cfg/cfg004.go:24,29-31,158-161` | Decimal SI memory suffixes (`512M`) and bare numbers (`memory: 1`) are treated as authoritative and reported out of range. The schema defines only a pattern; how the service reads them is unverified. | Report `M`/`G` suffixes and bare numbers as uncertain. |
| R9 | minor | `cfg/versions.go:139-169` | A lower bound above the tested window (`>=1.0.0`, `~1.0.0`) passes silently; a comma-separated constraint is "cannot be parsed"; any floor under the tested minimum turns CFG-001 into a skip. | Note a floor above the tested maximum; accept commas; floors below the range are info only. |
| R10 | minor | CFG-001 / `wire.go` | An unsatisfied `requiredVersions.azd` is never reported (ADR-004 section 5; the azd schema says the project fails to load). | Add the finding or amend the ADR. |
| R11 | minor | `rules/catalog/**` | Verified windows not widened: azd 1.36.0 (2026-10-15) and azure.ai.agents beta.19/beta.20 exist; nothing runs on azd 1.36.0 (fail-safe, documented in `docs/tool-compatibility.md`). | Re-verify and widen, or keep the documented window. |
| R12 | minor | `rules/catalog/**` | 325 `verifiedVia: refs/...` lines in 105 of 108 catalogue files cite local clone paths. | Replace with URL plus date or immutable commit URL. |
| R13 | minor | `cfg/cfg005_012.go:230` | CFG-006 treats `AZURE_TENANT_ID`, `AZURE_PRINCIPAL_ID`, `AZURE_LOCATION` as always produced, which the catalogue says the source does not state. Skip reasons for no selected environment are bare `input-unavailable`. | Name the reason: `no-environment-selected`, `env-file-missing`, `default-environment-not-found`. |
| R14 | minor | `env/env.go`, `FND-ENV-001.yaml` | A literal `services.<name>.resourceGroup` is an error while multiple environments exist; the schema describes it as the group that contains the resource, so the service-level conclusion has no source. | Restrict the error to the top-level key, or make it uncertain. |
| R15 | minor | `docs/configuration.md` | The example `profile: prod` is rejected (`foundry-dev|test|prod` only). | Fix the docs. |
| R16 | nit | `app/providers.go`, CFG-006 | A key with an empty value counts as a producer. | Treat empty as uncertain. |

## Status of earlier findings

Fixed: N1, N2, N3, N4, N5, N6, N7, N9, N11, N13, N15, N16, N17, N19, N21, N22, B1, B2, M3, M4, M5, m1, m2, m3, m4, m8, m9.
Partly fixed: N8 (R9), N10 (R3), N12 (R1), N18 (R5), N20 (R13), B3 (R10, N13), M1 (R2).
Not fixed: N14 (R11), M2 (R7), m5 (multi-document azure.yaml, unverified), m6 (ADR-004 section 4 still mentions the process environment), n1 (R12).

## Unsupported assumptions still in the code

Decimal SI memory suffix normalisation; service-level literal `resourceGroup` as an error; `AGENT_<SERVICE>_*` name normalisation (`-` and `.` to `_`); name-only secret heuristics including the `sk-` shape; the `AZURE_LOCATION` casing rule; OPS-008 treating any "production" text as a promotion marker and requiring `eval.yaml` in the project directory; `.azure/config.json` `defaultEnvironment` as azd's selection (plausible, not verified from Learn). The Aspire.Hosting.Foundry docs state a different hosted-agent resource rule (cpu 0.5-3.5, memory 1-7, memory equal to twice cpu); the reviewer could not reconcile it with the Learn 0.25-4.0 cores and 0.5-8.0Gi bounds.

## Missing tests

CFG-005 names containing ACCOUNT, PROFILE, DESIGN; CFG-006 protocol endpoints and non-agent services; ENV-002/003 with secret-named keys holding resource IDs; fingerprint stability when a second finding appears; equal-evidence findings with different locations kept; baseline round trip; OPS-007 `echo azd up`, trailing comment, dead code; `--strict` per skip head per rule; constraints above the window and comma-separated; unsatisfied `requiredVersions.azd`; empty-valued `.env` key.

## DoD items unmet

No false pass or false fail: R2, R3, R4, R5, R14. Findings and baselines stable: R1. Skipped never passed and `--strict` contract: R3, R6. Every fact has a verified source: R7, R8, R12, R13. Unknown versions fail safely: met.
