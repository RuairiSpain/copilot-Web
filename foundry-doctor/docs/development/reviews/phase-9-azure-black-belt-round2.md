# Phase 9 review: Azure black belt, round 2

Reviewer: `azure-black-belt` (independent; did not write the code). Date: 2026-10-18. Round 2.
Scope: same as round 1 ([phase-9-azure-black-belt-round1.md](phase-9-azure-black-belt-round1.md)), re-checked against fix log [phase-9-fixlog-loop1.md](phase-9-fixlog-loop1.md). The concurrent phase-4 edits to `internal/assess` and the report renderers are out of scope.

## Environment constraints

- No Go toolchain and no package registry access, so `go build`, `go vet` and `go test` were not run.
- Compile correctness was checked by reading imports, signatures, callers and test helpers.
- Microsoft Learn claims were re-checked with `web_fetch`; the URLs are cited below.

## Round-1 findings: status

| Id | Status | Evidence |
|---|---|---|
| B1 `AZURE_ACCESS_TOKEN` forwarded to inference host | **CLOSED** | `azure.NewCLICredentialForTenant` (`internal/azure/auth.go`) builds the normal chain and then removes the `env` source; the default chain is unchanged. `llm.NewAzureOpenAIProvider(nil, nil)` uses it, and `newNarrator` (`internal/app/llm_narrator.go`) passes `nil, nil`. The `net/http` and `internal/azure` imports were removed from that file and nothing still uses them. Tests: `auth_cli_test.go` `TestCLICredentialIgnoresEnvironmentToken` (helpers `scriptRunner`, `azdOut` and `calls` exist in `auth_test.go`, same package) and `TestAzureOpenAIProviderNeverForwardsEnvironmentToken` (the `run` closure matches the `CommandRunner` signature). `docs/llm.md` now says the env token is never used and that the advisory falls back when only that variable is present. |
| B2 scope accepts any audience | **CLOSED** | `allowedScopes` = {`https://ai.azure.com/.default`, `https://cognitiveservices.azure.com/.default`} is enforced in `ResolveConfig` and again in `Complete` before `Token`. Tests: `TestResolveConfigScopeAllowList` (ARM, Graph and missing-`/.default` cases), `TestAzureOpenAIProviderRejectsPortAndScope`, and `TestLazyNarratorRejectsNonAllowListedScope` (provider never called). The default audience matches Learn: "Tokens must be issued with scope https://ai.azure.com/.default" (<https://learn.microsoft.com/en-us/azure/foundry/foundry-models/concepts/endpoints>). Learn's v1 REST samples still mint with `--resource https://cognitiveservices.azure.com` (<https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/reasoning>), so allow-listing that audience is reasonable. Marking it UNVERIFIED in the docs is conservative and acceptable. |
| B3 redirects bypass allow-list | **CLOSED** | `refuseRedirect` returns `http.ErrUseLastResponse`. It is installed on the default client and forced on a copy of any caller client (`client := *p.Client; client.CheckRedirect = refuseRedirect`). Any 3xx returns `ErrUnavailable`. Copying `http.Client` is safe because it has no lock fields. Go behaviour: <https://pkg.go.dev/net/http#Client>. Tests: `TestAzureOpenAIProviderRefusesRedirects` (asserts one transport call and that the host is never the redirect target) and `TestNewAzureOpenAIProviderDefaultClientRefusesRedirects`. |
| B4 hallucinated rule IDs filtered, free text unchecked | **CLOSED** | `cleanRules` now returns `([]string, error)` and rejects any unknown ID. Its single caller was updated. `checkMentionedRules` scans the top-level summary and each section heading, summary and actions with `\bFND-[A-Z0-9]+-[A-Z0-9]+\b` (case-insensitive). I checked that every catalogue rule ID has the shape `FND-<area>-<nnn>` (108 IDs under `rules/`), so the token regex cannot partially match a real ID. Tests: `TestNarrativeRejectsUnknownRuleIDsAnywhere` (mixed `ruleIds`, top summary, lower-case section summary, heading), `TestNarrativeAllowsKnownRuleIDInText`, and the end-to-end `TestLazyNarratorValidatesProviderOutput`. FND-CFG-001 exists, and a zero `lazyCatalog{}` loads the embedded catalogue through `once.Do`. |
| B5 skip reasons carry ARM IDs, GUIDs and hosts | **CLOSED** (residual N-R1) | `clean` redacts `/subscriptions/...` and GUIDs in all projected text, and `redactHosts` removes DNS names from skip reasons. `TestAssessmentProjectionRedactsSkipReasonIdentifiers` traces correctly: the ARM regex consumes through `acct1`, both GUIDs become `<id>`, and `myacct.openai.azure.com` becomes `<host>`. `infra/main.bicep` survives because `bicep` is not in the TLD list, and the result fits within 320 bytes. |
| B6 hand-off accuracy | **CLOSED** | `samples/llm/` now exists with a README and five JSON files that match the real DTO and validator. I checked that `owner-response-good.json` contains no prohibited-claim terms and covers both input IDs with verbatim actions. The false "Approved" line was removed, test runs are explicitly marked NOT RUN, and the ADR-019 reviews and PRD 22 rows are disclosed as unmet. |
| N1 output cap | **CLOSED** | `max_completion_tokens: 4096` is sent. `TestAzureOpenAIProviderRequestBodyKeys` asserts the exact key set. `finish_reason: length` is rejected with a specific note. Per Learn, this cap includes reasoning tokens (reasoning URL above). |
| N2 temperature | **CLOSED** | `temperature` is no longer sent (the key-set test enforces this), and the reason is documented. |
| N3 data residency | **CLOSED** | Documented in `docs/llm.md`. Source: <https://learn.microsoft.com/en-us/azure/foundry/foundry-models/concepts/deployment-types> |
| N4 roles | **CLOSED** | "Foundry User (formerly Azure AI User)" re-verified today: Learn states the rename and says that to call a deployment at inference time you should "assign the Foundry User role on the Foundry account scope" (<https://learn.microsoft.com/en-us/azure/foundry/concepts/rbac-foundry>). Cognitive Services OpenAI User for classic Azure OpenAI resources is cited to the classic RBAC page. |
| N5 network docs | **CLOSED** | `docs/llm.md` now covers proxy variables, private endpoints with the 403 fallback, rejected sovereign-cloud hosts, and refused redirects. |
| N6 tenant selection | **OPEN, accepted** | No tenant override yet; disclosed as a known limitation with a sign-in workaround. Not blocking. |
| N7 explicit ports | **CLOSED** | `u.Port() != ""` is rejected, and there is a test. |
| N8 byte-wise truncation | **CLOSED** | `runePrefix` and `utf8.RuneStart` back-off in `redact.truncate` and `validate.sanitize`, with tests. |
| N9 prohibited-claim breadth | **PARTIALLY** | `upgrade` was removed. Bare `priority`, `severity` and `urgent` are kept as a fail-closed choice and disclosed. Acceptable. |
| N10 run-metadata doc claim | **CLOSED** | The docs now list only audience. |

## New findings introduced by the fixes

No blocking issues. Compile-read checks:

- **Imports.** `provider_azure_openai_test.go` uses every import (`bytes`, `base64`, `json`, `sort`, `errors`, `time`, `azure`). The test files for `validate`, `redact`, `config`, `auth_cli` and `llm_narrator` import only what they use. `llm_narrator.go` no longer imports `net/http` or `internal/azure`.
- **Fields and constants.** `llm.Config.MaxOutputTokens` is declared in `types.go`. `LegacyScope` and `DefaultMaxCompletionTokens` are exported constants. The `cmd` test clears the LLM environment variables with `t.Setenv(k, "")`.

Non-blocking:

- **N-R1. Partial resource-name redaction.** Skip-reason redaction covers ARM IDs, GUIDs and FQDNs, but a bare resource name in prose (for example `account 'contoso-aoai' returned 403`) still passes through. This is lower sensitivity than a full ARM ID and is acceptable for the advisory path. Consider sending skip reason *codes* instead of free text in a later phase.
- **N-R2. ID collisions with catalogue cross-references.** If a catalogue description or recommendation cites another rule ID and the model repeats it, the response is rejected. This fails closed (the advisory becomes unavailable, never a false claim), but it adds to the unmeasured false-rejection rate already disclosed.
- **N-R3. Lost managed-identity path.** Removing `AZURE_ACCESS_TOKEN` means CI and managed-identity runs cannot use `--llm-explain`; this is disclosed. A future scope-bound env variable (for example `FOUNDRY_DOCTOR_LLM_ACCESS_TOKEN`, checked for `aud`) would restore that path safely.

## Conditions outside this reviewer's remit

These are disclosed, not blocking for the Azure integration review:

- The maintainer must run `go build`, `go vet`, `go test -race ./...` and `verify-phase.sh --strict` on this tree.
- The ADR-019 Go principal engineer and security reviews are still NOT DONE. The hand-off records the PRD 22 rows as unmet, so release remains gated.

## Verdict

All six round-1 blockers are CLOSED with code and test evidence. 14 of 16 round-1 items are closed, one is partially closed and accepted, one is open and accepted, and none of the fixes introduces a new blocking issue. This approval covers the Azure integration scope: Entra ID token audience, endpoint allow-list, egress and redaction, cost controls, and response validation. It assumes the test run listed above passes.

VERDICT: APPROVE
