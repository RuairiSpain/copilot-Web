# CSA demo runbook

This runbook is for a customer-facing, read-only Foundry Doctor demo. Use it to
show offline validation first, then explain, compare, assessment, graph,
annotate, and—only with prerequisites ready—preflight and runtime diagnosis.

## Before the demo

### Required for every demo

- Foundry Doctor built or installed
- sample tree from `samples/`
- no live Azure permissions required for offline commands

### Required for preflight/runtime

- Azure login already established via `AZURE_ACCESS_TOKEN`, `azd`, or `az`
- target subscription, resource group, and where applicable Foundry account and
  project names
- for `runtime --vantage vnet`, execution from a network vantage point that can
  resolve the target private DNS records

## Install

### Standalone binary

```bash
foundry-doctor version
```

Expected highlights:

- printed tool version
- config path `.foundry-doctor/config.yaml`
- minimum azd `1.34.2`
- minimum Bicep `0.48.1`

### azd extension

After the release registry is published:

```bash
azd extension source add -n foundry-doctor -t url -l <registry.json-url>
azd extension install ruairispain.foundry-doctor --source foundry-doctor
azd foundry version
```

Use the standalone binary for the demo if the custom azd extension source is
not pre-staged on the demo machine.

## Offline demo sequence

### 1. Happy path: clean local project

```bash
foundry-doctor doctor --local --dir samples/good
```

Expected result:

- exit `0`
- no findings
- no Azure login required

### 2. Broken sample: immediate value

```bash
foundry-doctor doctor --local --dir samples/bad
```

Expected result:

- findings are reported
- exit `1`
- unresolved variables and YAML/config defects appear as findings or skips, not
  silent passes

### 3. Rule explanation

```bash
foundry-doctor explain FND-CFG-001
```

Expected result:

- deterministic explanation with sources
- no live Azure dependency

### 4. Compare environment policy

```bash
foundry-doctor compare dev prod --dir <customer-repo>
foundry-doctor compare dev prod --dir <customer-repo> --fail-on-diff
```

Expected result:

- first command is informational and exits `0` even when environments differ
- second command exits non-zero only when differences exist

### 5. WAF assessment

```bash
BICEP_PATH=/absolute/path/to/bicep foundry-doctor assess waf --dir samples/assess-prod --audience owner
```

Expected result:

- owner-oriented WAF summary
- skipped and uncertain evidence remains visible

### 6. Graph

```bash
foundry-doctor graph --source --dir samples/good --format mermaid
```

Expected result:

- Mermaid flowchart text
- no live Azure access

### 7. Annotate

```bash
foundry-doctor annotate --dir samples/good --format review --out review/demo.review
```

Expected result:

- review artefacts written under `review/demo`
- suitable for code-review-style walkthroughs

### 8. Cost

```bash
BICEP_PATH=/absolute/path/to/bicep foundry-doctor cost --dir samples/cost-fixed --offline
```

Expected result:

- advisory monthly estimate
- explicit exclusions for anything not modelled

## Live Azure sequence

### 9. Preflight

```bash
foundry-doctor preflight --subscription <sub> --resource-group <rg> --location <loc> --what-if
```

Expected result:

- readiness summary with `ready`, `blocked`, `uncertain`, and `skipped`
- no mutations
- missing permissions degrade to skipped checks, not passes

### 10. Runtime diagnosis

```bash
foundry-doctor runtime --subscription <sub> --resource-group <rg> --account <account> --project <project>
```

Expected result:

- runtime findings over metadata-only probes
- no prompts, completions, or document bodies retrieved

## Honest limitations to say out loud

- Foundry Doctor is read-only and advisory; it does not guarantee deployment or
  runtime success.
- `compare` is informational by default; use `--fail-on-diff` only when policy
  drift should break automation.
- Preflight and runtime depend on the caller already having Azure access.
- The azd extension uses a custom extension source, not the Microsoft-owned
  default registry.
- Bicep support is verified at `0.48.1`; older versions are rejected clearly,
  not silently tolerated.
- Some offline commands still require the Bicep CLI when the sample depends on
  compiled infrastructure, even though they do not require Azure credentials.

## Validation note for this workspace

This runbook reflects the verified V1 command surface. The documented offline
commands above were exercised in this workspace; Azure-backed `preflight` and
`runtime` remain prerequisite-dependent and were not executed here.
