# Phase 1 re-review: Foundry lead (code from `main`)

Reviewer: `foundry-lead` (independent). Date: 2026-10-15. Code: branch `claude/foundry-doctor-phase-1` (main's implementation, PR #18). Verdict: **REJECT**.

Method: binary built with Go 1.25.13, run against scratch projects with a stub `azd`; ENV rules exercised with in-memory environments (no `.env` files could be written). Learn pages were unreachable; schema claims rest on the azure-dev repository schemas.

Unmet DoD items: "rules must not false-pass or false-fail" (N1-N9, B1, B2, M1, M3-M5, m4, m9); "every fact has a verified source" (N1, N3, N6); "skipped never equals passed" (M4, M5, N10); "unknown versions fail safely" (partly, B3).

## Findings

| id | sev | where | issue | fix |
|---|---|---|---|---|
| N1 | blocker | `internal/rules/cfg/cfg004.go:93-103` | CFG-004 reads `cpu`/`memory` at service level. Schema puts them at `container.resources.{cpu,memory}`. A hosted agent with `cpu: "64"` passes; a top-level `memory: 512Mi` false-fails because only `Gi` is accepted. The unit test uses the wrong shape. | Read `container.resources.*`; normalise memory units; fix the test. |
| N2 | blocker | `cfg004.go:79-82` | "No Dockerfile and no image or codeConfiguration" errors on the reference example (`runtime` + `startupCommand`). | Accept `runtime`, `startupCommand`, `skill`; otherwise uncertain. |
| N3 | blocker | `cfg004.go:52-56`, `cfg002_003.go:121-128` | Prompt agent `model` is reported "not a deployment of project" when the service has no `deployments` key; `deploymentNames` returns `known=true` for an empty map. | Return `known=false` when `deployments` is absent. |
| N4 | blocker | `cfg005_012.go:171-208` (CFG-006) | Any `${VAR}` not in `.env`, four standard variables or ARM outputs is an error, also when `in.ARM == nil`. Catalogue says post-provision outputs are uncertain. | Skip or uncertain without ARM or before provision; extend the standard set. |
| N5 | blocker | `internal/rules/ops/ops.go:286-291` (`repoFiles`) | `os.ReadFile` with no size limit, follows symlinks outside the project (`/dev/zero` causes out-of-memory). OPS-008 shares it. | Open through `os.Root`, skip symlinks, cap size and file count. |
| N6 | major | `cfg001.go:90-93` | CFG-001 errors when `services` is absent (schema: optional). `name` presence and pattern are not checked. | Drop the services error; check `name`. |
| N7 | major | `cfg/common.go:57-66`, `walkScalars` | cfg helpers ignore YAML merge keys and aliases; `azureyaml` follows them. | Resolve once in `azureyaml`, expose the resolved view. |
| N8 | major | `cfg001.go:57-66` | `requiredVersions.extensions` check takes the first `x.y.z` in the constraint; `<1.0.0-beta.12` and `>=1.0.0-beta.10` are errors. | Parse constraints; floor below tested range is info. |
| N9 | major | `cost/cost.go:74-76`, `armmodel` | ARM `condition` ignored: a `condition:false` budget passes COST-001; a conditional-false resource is flagged by COST-002. | Evaluate literal conditions; otherwise unresolved. |
| N10 | major | `env/env.go:84-144`, `app/providers.go:166-197` | ENV rules pass silently when `.env` is missing, unreadable, over-size, truncated at 2000 keys, or `AZURE_ENV_NAME` is equal or empty (M3, M4, M5). | Carry per-environment status; skip with a named reason or uncertain finding. |
| N11 | major | `env/env.go:105-111` | `_ID`/`_NAME` suffix test flags `AZURE_TENANT_ID` and `MODEL_DEPLOYMENT_NAME`. | Exclude those; make remaining hits uncertain. |
| N12 | major | `internal/findings/findings.go:19-24` | Fingerprint ignores evidence and location: CFG-005/006 findings on one service share one fingerprint. | Add a stable discriminator (key or variable name). Changes fingerprints, needs an ADR note. |
| N13 | major | `app/wire.go:495-518`, `app/azd.go` | Without `azd` on PATH every versioned rule skips as `input-unavailable`, exit 0; no flag to give a version. | Reason `azd-version-unknown`; fall back to `requiredVersions.azd`; flag; header note. |
| N14 | major | `rules/catalog/**` | Ranges stop at azd `<=1.36.0-beta.1` and agents beta.18; azd 1.36.0, agents beta.20 exist. All rules skip (fail-safe, stale data). | Re-verify and widen, or document the window. |
| N15 | major | `cfg005_012.go:160-168` | CFG-006 reports `.env` keys with `FOUNDRY_`/`AGENT_` prefix as reserved although azd writes `AGENT_<SERVICE>_*`; no source applies the restriction to `.env`. | Remove or exempt. |
| N16 | minor | `cfg005_012.go:25,49-58` | `sv` (SAS version) treated as a credential. | Require `sig`. |
| N17 | minor | `cost.go:26-30,74-76` | Catalogue says external budget is skipped; code fails. Forecast notification and scope checks missing. | Align. |
| N18 | minor | `ops.go:636-645` | Passes on any text containing `azd ` (comments, non-YAML); `azure-dev.yml` without `azd ` skipped. | Parse YAML `run:` steps. |
| N19 | minor | `app/wire.go` | ADR-004 reasons `synthetic-infrastructure`, `unsupported-host`, `invalid-azure-yaml` not emitted. | Emit or amend ADR. |
| N20 | minor | `internal/project/project.go` | `.azure/config.json` `defaultEnvironment` never read. | Honour or state in the skip reason. |
| N21 | minor | `app/providers.go:166-197` | `.env` parser is not the godotenv grammar the catalogue claims. | Use godotenv or document. |
| N22 | minor | `env/env.go:76-79` | `LiteralRGInYAML` checks only top-level `resourceGroup`. | Implement or reduce the catalogue claim. |

## Checklist from the earlier review

| id | status |
|---|---|
| B1 | present (`ops.go:636` uses the azure.yaml directory, not the git root) |
| B2 | present (`pipeline:` section passes, `ops.go:630-632`) |
| B3 | partly present (azd version detected and gated; extension ranges never compared; `requiredVersions.azd` never read; no `unsupported-host`) |
| M1 | present (`TOKEN_LIMIT`, `MAX_TOKENS`, `KEYWORD_FILE` fail) |
| M2 | present, narrowed (`AZURE_LOCATION` with uppercase or space is an error) |
| M3 | present |
| M4 | present (variant) |
| M5 | present |
| m1 | present |
| m2 | absent |
| m3 | absent |
| m4 | present |
| m5 | present, unverified |
| m6 | present |
| m7 | not applicable |
| m8 | present (deployment and APIM SKUs case-sensitive) |
| m9 | present (`${KEY:-sk-realsecretvalue123}` passes) |
| n1 | present (about 40 `verifiedVia: refs/...` values) |

## Assumptions with no verified source

ARM rejecting display-name locations (CFG-012); secret-looking names as credential evidence (CFG-005); reserved prefix applying to `.env` keys (CFG-006); `sv` as secret; `azd ` in workflow text meaning deployment (OPS-007); multi-document azure.yaml being an azd error; `_ID`/`_NAME` classification (ENV-002); COST-002 SKU case sensitivity; a missing budget in the plan being a defect; skip reason and exit 0 when `azd` is absent.

## Missing tests

CFG-004 (`container.resources`, `Mi`, `runtime`/`startupCommand`, no `deployments`); CFG-001 (no `services`, no `name`, bad name, `<`/`>=`/`~` constraints); merge keys and aliases through all CFG rules; CFG-005 names and `${VAR:-literal}`; CFG-006 standard variables, no ARM model, `AGENT_<SERVICE>_*`; CFG-012 `East US`; ENV-001..004 missing/oversize/unparseable `.env`, over 2000 keys, equal `AZURE_ENV_NAME`, no group or subscription; OPS-007 nested azure.yaml, `pipeline:` only, `azd` in a comment, symlink and oversize workflow; COST-001/002 `condition: false`, lower-case SKU; version gating with declared `requiredVersions`; fingerprint uniqueness; multi-document azure.yaml.

## Verified correct

Project discovery path safety (symlink and oversize azure.yaml refused); azd version range handling at tested boundaries; secret values from `.env` replaced by a digest; hostile alias-expansion YAML ends in 0.07 s without a crash.

## Limits of the review

No `.env` files were created, so the CLI was not run end to end on `.env` content. Learn pages were unreachable. `internal/app` tests `TestAnnotateRejectsUnsafeNonReviewOut` and `TestAnnotateReviewWritesCopy` fail (outside scope, listed in `phase-1-reconciliation.md`).
