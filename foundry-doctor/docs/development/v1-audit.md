# V1 demo-safety audit

Date: 2026-10-06

This document records the V1 demo-safety hardening work completed in this
workspace, what was actually exercised, the defects fixed in-tree, and the
remaining limitations that are still stated honestly.

## Goal

Make the V1 demo path safer on Windows:

- no crashes or panics on malformed or missing local inputs
- skipped or uncertain checks never reported as passes
- Azure-backed commands fail closed when prerequisites are unavailable
- realistic good/bad sample coverage for Bicep-backed Foundry projects
- deterministic repeated output for the documented offline commands

## Commands exercised in this workspace

The following command families were exercised during this pass against the
sample tree and/or package-level command tests:

- `version`
- `doctor`
- `explain`
- `compare`
- `assess waf`
- `graph`
- `annotate`
- `cost`
- `preflight`
- `runtime`

Observed/verified behaviors from direct execution and focused command tests:

- `doctor --local` on offline samples keeps skips visible instead of converting
  them to passes
- `preflight` and `runtime` skip-only runs now exit unavailable instead of
  looking like success
- `graph --source` remains offline and read-only
- `annotate` supports `review`, `github`, and `sarif`
- `cost --offline` remains advisory and does not invent missing pricing inputs

## Bugs fixed

### 1. Skip-only preflight/runtime runs looked like success

Problem:

- when every live Azure check skipped because permissions, credentials, or
  runtime prerequisites were missing, `preflight` and `runtime` could still
  produce a success exit code

Fix:

- mark rendered `preflight` and `runtime` skips as required before exit-code
  classification

Effect:

- skip-only Azure-backed runs now exit `2` (`unavailable`) instead of `0`

Files:

- `internal/app/preflight.go`
- `internal/app/runtime.go`
- `internal/app/preflight_test.go`
- `internal/app/runtime_test.go`
- `cmd/foundry-doctor/preflight_test.go`
- `cmd/foundry-doctor/runtime_test.go`

### 2. ARM loading depended too narrowly on `infra/main.bicep`

Problem:

- local analysis could drift from documentation and sample expectations when
  only compiled ARM JSON was available

Fix:

- resolve the Bicep entry via `azure.yaml`
- if the Bicep entry is absent, fall back to adjacent compiled ARM JSON

Effect:

- doctor/preflight/runtime/cost/graph can consume checked-in ARM JSON fixtures
  without silently treating the project as if no infrastructure existed

Files:

- `internal/app/wire.go`
- `internal/app/app_test.go`

### 3. Compiled ARM child/scoped resources were too opaque

Problem:

- valid compiled ARM often preserved literal expressions such as
  `format(...)` or `resourceId(...)` in resource names/scopes and some property
  fields, which made deterministic rules miss legitimate relationships or treat
  valid samples as unresolved

Fix:

- added conservative normalization for simple literal-only ARM expressions in
  resource names, scopes, and property values

Effect:

- diagnostic settings, child resources, and DNS references coming from compiled
  ARM can now be matched more reliably

Files:

- `internal/armmodel/armmodel.go`
- `internal/armmodel/armmodel_test.go`
- `internal/rules/net/rules.go`
- `internal/rules/net/rules_test.go`

### 4. Runtime diagnostic-settings path allow-list regression

Problem:

- runtime probing of diagnostic settings could be rejected by the Azure
  transport allow-list

Fix:

- added a regression test covering the runtime diagnostic-settings path

Files:

- `internal/azure/transport_test.go`

### 5. Data-plane SSRF and bearer-token redirect risk

Problem:

- data-plane clients could build hosts from unvalidated Search / Foundry names
- the shared HTTP helper allowed redirects, which risked forwarding bearer
  tokens to attacker-controlled hosts

Fix:

- validate Search service, Foundry account, and Foundry project names against a
  strict lowercase host-label regex before building endpoints
- require expected host suffixes in the shared runtime HTTP helper
- disable redirects with `CheckRedirect = http.ErrUseLastResponse`

Effect:

- hostile values such as `x.evil.com#`, `..`, `@`, ports, uppercase names, and
  unicode names are rejected before request dispatch
- bearer tokens are not forwarded across redirects

Files:

- `internal/runtime/host.go`
- `internal/runtime/http.go`
- `internal/runtime/search/search.go`
- `internal/runtime/iq/iq.go`
- `internal/runtime/foundry/foundry.go`
- `internal/runtime/monitor/http.go`
- `internal/rules/run/run.go`
- `internal/runtime/http_test.go`
- `internal/runtime/host_test.go`
- `internal/runtime/search/search_test.go`
- `internal/runtime/iq/iq_test.go`
- `internal/runtime/foundry/foundry_test.go`

## Sample audit work

### Added realistic private-networked fixtures

New samples:

- `samples/good-private/`
- `samples/bad-private/`

These samples model a more realistic Foundry deployment shape than the minimal
`good/` and `bad/` fixtures:

- Foundry account/project/deployment
- capability settings
- private networking and agent subnet delegation
- Search, Storage, Cosmos, Log Analytics
- private endpoints / private DNS
- diagnostics and tagging policy inputs
- evaluation and workflow artefacts

### Good sample intent

`samples/good-private/` is intended to be a realistic, well-configured,
private-networked project that should not raise error/warning findings from the
deterministic verified rules it satisfies statically.

### Bad sample intent

`samples/bad-private/` is a targeted regression twin with injected defects:

- Search replica count reduced
- storage shared-key access enabled
- Foundry local auth enabled

Expected targeted findings:

- `FND-REL-001`
- `FND-SEC-001`
- `FND-SEC-004`

## Test additions and regression coverage

Added or extended coverage for:

- ARM JSON fallback loading
- skip-only exit semantics for `preflight` and `runtime`
- deterministic offline report behavior
- compiled ARM literal normalization
- runtime diagnostic-settings allow-list behavior
- good/bad private-sample end-to-end doctor expectations
- command-level format coverage for `annotate`, `graph`, and `cost`

## Documentation updates

Updated:

- `samples/README.md`

This now documents the private-networked good/bad sample pair alongside the
existing minimal fixtures.

## Validation status

Focused Go test packages were run successfully earlier in this pass for the
directly changed rule/app areas:

- `./internal/app`
- `./internal/azure`
- `./internal/armmodel`
- `./internal/rules/net`

Later in the pass, additional command-test updates were made. A fresh
post-change rerun of the full requested gate set was attempted but is currently
blocked in this workspace by a repository pre-tool hook error that prevents
PowerShell command execution.

## Remaining limitations

These are still stated honestly:

- Azure-backed `preflight` and `runtime` remain prerequisite-dependent; they
  cannot prove readiness when credentials, permissions, or required vantage
  conditions are absent
- some rules still depend on what compiled ARM can determine statically; when
  the template leaves values unresolved, the result must remain skipped or
  uncertain rather than pass
- this document does **not** claim a clean rerun of every final acceptance gate
  after the last test additions, because command execution became blocked by the
  workspace hook

## Next required verification once the hook is clear

Run from `foundry-doctor/`:

- `gofmt -l .`
- `go vet ./...`
- `go test -count=1 ./...`
- `staticcheck ./...`
- `govulncheck ./...`
- `bash scripts/claude/check-no-secrets.sh`
- `bash scripts/claude/check-rule-catalog.sh`

And re-exercise the documented Windows command matrix in:

- `docs/demo/README.md`
- `README.md`
