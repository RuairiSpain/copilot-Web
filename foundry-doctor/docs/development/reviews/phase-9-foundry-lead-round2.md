# Phase 9 review: foundry-lead, round 2

- Phase: 9, optional LLM explanations and executive narrative (PRD section 17)
- Reviewer: `foundry-lead` (read-only; did not author any of this work)
- Date: 2026-10-18
- Inputs:
  - `REVIEW_BRIEF.md`
  - `phase-9-foundry-lead-round1.md`
  - `phase-9-fixlog-loop1.md`
  - the current tree under `/mnt/workspace/working/canon/foundry-doctor`
- Environment constraints:
  - No Go toolchain and no registry access. Nothing was compiled or run.
  - I checked compile-correctness by reading imports, signatures, callers and the test helpers each test relies on.
  - I also ran a Python simulation of the validator rules over `samples/llm/owner-response-good.json`.
- Out of scope:
  - Concurrent phase-4 edits under `internal/assess` and the report renderers.

## Part A. Round-1 findings

### Blocking

| ID | Status | Evidence |
|---|---|---|
| B1 `AZURE_ACCESS_TOKEN` forwarded to the inference host | **CLOSED** | See B1 notes below. |
| B2 `samples/llm/` missing | **CLOSED** | See B2 notes below. |
| B3 ADR-019 obligation not addressed | **CLOSED (disclosed)** | See B3 notes below. |
| B4 free text could introduce findings | **CLOSED** | See B4 notes below. |

**B1 evidence**
- `internal/azure/auth.go` `NewCLICredentialForTenant` (lines 96–112) builds the normal chain, then filters out the `env` source.
  - The `azd` and `az` sources stay scope-specific (`--scope` / `--resource`).
  - `NewDefaultCredential` is unchanged, so other commands are unaffected.
- `NewAzureOpenAIProvider(nil, nil)` defaults to that credential. It is the only production caller (`internal/app/llm_narrator.go:30`).
- The `internal/app/llm_narrator.go` imports no longer include `net/http` / `azure`. No other code uses those imports.
- Tests that cover it:
  - `auth_cli_test.go` uses the existing `scriptRunner` / `azdOut` helpers from `auth_test.go`. Their signatures match.
  - `provider_azure_openai_test.go` `TestAzureOpenAIProviderNeverForwardsEnvironmentToken` asserts that the header is `Bearer tok-ai-audience` and that the azd `--scope` is `DefaultScope`.
  - Note: terminal output masks this literal as `******`. I checked the file's characters directly, and they are correct.
- `docs/llm.md` §auth now states that `AZURE_ACCESS_TOKEN` is never used.

**B2 evidence**
- `samples/llm/` exists and contains:
  - a README;
  - `projected-request.json`;
  - `provider-request-shape.json`;
  - one accepted response and two rejected responses.
- I replayed the validator rules over the "good" response against the projected request: no prohibited claim, no unknown ID, actions copied verbatim from the recommendations, and both IDs covered.

**B3 evidence**
- The hand-off now has "Go principal engineer review (ADR-019)" and "Security review (ADR-019)" sections.
  - Both are marked **NOT DONE**.
  - Both are stated as required before any release tag.
- The PRD 22 rows are in the DoD table as **unmet**.
- I did not perform these reviews. They remain an open project obligation (see Part C).

**B4 evidence**
- `internal/llm/validate/validate.go`:
  - `ruleIDToken` is `(?i)\bFND-[A-Z0-9]+-[A-Z0-9]+\b`.
  - `checkMentionedRules` runs on the summary, each heading, each section summary and each action.
  - `cleanRules` now returns an error on any unknown ID instead of dropping it.
- The grammar matches the catalogue: all 108 IDs are 3-part `FND-X-NNN`.
- Tests:
  - unit test `TestNarrativeRejectsUnknownRuleIDsAnywhere` (mixed `ruleIds`, top summary, lower-case in a section summary, heading);
  - `TestNarrativeAllowsKnownRuleIDInText`;
  - end-to-end test `TestLazyNarratorValidatesProviderOutput`. It goes through the real `redact` → `prompts` → `validate` pipeline. The zero-value `lazyCatalog{}` loads the embedded catalogue through `sync.Once`, which is valid.

### Non-blocking

| ID | Status | Evidence |
|---|---|---|
| N1 scope allow-list and both audiences documented | **CLOSED** | See N1 notes below. |
| N2 endpoint forms | No change needed | Ports are also rejected now (`providerURL`, `u.Port() != ""`). |
| N3 refusal / `finish_reason` | **CLOSED** | `Complete` checks `message.refusal`, `length` and `content_filter`, each with a distinct `ErrRejected` note. Tested. |
| N4 `temperature` / output cap | **CLOSED** | `temperature` is removed. `max_completion_tokens` comes from `cfg.MaxOutputTokens` (4096). `TestAzureOpenAIProviderRequestBodyKeys` asserts the exact key set. Default timeout is still 10s and can be set up to 30s. That is acceptable. |
| N5 prohibited-claim breadth | **PARTIALLY** | `upgrade` is removed. Bare `priority` / `severity` / `urgent` are kept as fail-closed and disclosed in Known limitations. Accepted. |
| N6 CLI test env isolation | **CLOSED** | `cmd/foundry-doctor/main_test.go` `exec` clears `FOUNDRY_DOCTOR_LLM_*`, `AZURE_OPENAI_*` and `AZURE_ACCESS_TOKEN`. |
| N7 ambient `AZURE_OPENAI_ENDPOINT` | **CLOSED (docs)** | Warning added in `docs/llm.md`. |
| N8 "run metadata" claim | **CLOSED** | `docs/llm.md` says that no other run metadata is sent. This matches `AssessmentInput`. |
| N9 hand-off review links | **CLOSED** | Both round-1 files are linked. The unsupported "black belt approved" line is withdrawn. |

**N1 evidence**
- `llm.allowedScopes` contains only `https://ai.azure.com/.default` and `https://cognitiveservices.azure.com/.default`.
- The allow-list is enforced in two places:
  - in `ResolveConfig`;
  - in `Complete`, before any token is requested.
- Tests: `TestResolveConfigScopeAllowList`, `TestAzureOpenAIProviderRejectsPortAndScope`, `TestLazyNarratorRejectsNonAllowListedScope`.
- The default scope matches Learn: "Tokens must be issued with scope https://ai.azure.com/.default" (<https://learn.microsoft.com/en-us/azure/foundry/foundry-models/concepts/endpoints>).
- Acceptance of the `cognitiveservices.azure.com` audience is correctly marked UNVERIFIED.

### Missing tests from round 1

1. **Closed.** ARM env token never forwarded.
2. **Closed.** Unknown IDs in free text or `ruleIds` are rejected.
3. **Closed.** End-to-end `lazyNarrator` test. The fake "Priority" heading in `llm_test.go` is replaced.
4. **Closed.** Scope allow-list test.
5. **Closed.** Refusal / `length` tests.
6. **Open, deferred and disclosed.** HTML insertion point and evidence-pack byte identity are deferred because the phase-4 renderers are being changed concurrently. This is listed under "Deferred issues" in the hand-off. Acceptable, but add it in the next loop that touches the renderers.
7. **Closed.** CLI env isolation.

## Part B. New issues introduced by the fixes

No blocking issues were found. Compile-correctness checks (by reading):

### Compile-correctness checks

- **Exported names and fields.** `llm.Config.MaxOutputTokens` exists (`types.go:25`). `LegacyScope`, `DefaultMaxCompletionTokens` and `allowedScopes` are package-level and are used consistently by tests.
- **Copying `http.Client`.** `Complete` copies `*p.Client` into a local value and sets `CheckRedirect`. `http.Client` holds no mutex, so `go vet` copylocks is not triggered. The caller's client is not mutated.
- **Redirect handling.**
  - With `http.ErrUseLastResponse` the 3xx response is returned with its body.
  - The 3xx branch comes after the size-limited read, so the body is still closed by the `defer`.
  - `TestAzureOpenAIProviderRefusesRedirects` asserts that the transport is called exactly once.
- **Redaction imports.** `redact/project.go` uses every import it declares (`regexp`, `unicode/utf8` via `runePrefix`). `sdk.Skip{RuleID, Reason}` and `assess.Control.Skipped []sdk.Skip` match the test literals.
- **Validator imports.** `validate.go` still imports `bytes` (`MarshalCanonical`) and `utf8` (`sanitize`, `cleanText`), and the test imports `errors`, `llm` and `utf8`. All are used.

### New non-blocking findings

- **N10. Cross-references in catalogue text will cause fail-closed rejections.** Some catalogue texts name other rules (for example `FND-CFG-004` mentions `FND-CFG-001`, and `FND-DEP-011` mentions `FND-NET-006`). If such text reaches the projected `explanation` or `recommendation`, then:
  - a model that echoes it is rejected;
  - for `recommendation`, the verbatim action itself is rejected by `checkMentionedRules`.

  This fails closed (deterministic output is kept), so it is not a defect. Two possible fixes:
  - strip or neutralise foreign `FND-*` tokens during projection; or
  - add the referenced IDs to the allowed mention set (not to `ruleIds`).

  The false-rejection rate should be measured together with N5.
- **N11. `ruleAllowed` and `cleanRules` treat case differently.** `ruleAllowed` (free-text mentions) is case-insensitive, while `cleanRules` (`ruleIds` array) is case-sensitive. A model returning `"fnd-cfg-001"` in `ruleIds` is rejected. This is safe and consistent with the strict schema intent. Document it, or normalise to upper case.
- **N12. Hand-off and fix log disagree on the pre-review gates.** The hand-off's "Exact next-phase prerequisites" correctly require `go build` / `vet` / `test -race` / `verify-phase.sh --strict` before re-review, but none have run. This review is therefore a read-only approval. The maintainer must still run the gates. A red gate re-opens this review.

## Part C. Phase DoD (PRD §17) and ADR-019

| DoD item | Status |
|---|---|
| Default execution makes no model call | met (flags default off, narrator only on `LLMExplain`; tests exist, unexecuted) |
| No raw source files, secrets, prompts or document content are sent | met by code (allow-list DTO, ARM-ID/GUID/host redaction, CLI-only audience-scoped token, scope allow-list, redirects refused) |
| LLM output cannot add/remove findings or alter severity | met by code (unknown IDs rejected everywhere, coverage check, verbatim actions, prohibited claims; deterministic findings and exit codes untouched) |
| Reports label generated content and provider metadata | met |

ADR-019:
- The obligation is now **accounted for** in the hand-off. The go-principal-engineer and security-reviewer reviews are disclosed as not done and as release-blocking.
- The PRD 22 rows are honestly marked unmet.
- This approval covers the Phase 9 DoD and Foundry/platform correctness only. It does **not** satisfy the ADR-019 reviews or the PRD 22 release gate, and no release tag may be cut until those reviews run.

VERDICT: APPROVE
