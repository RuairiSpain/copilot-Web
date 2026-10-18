# ADR-026: Phase 4 WAF assessment scope, evidence states and deferrals

Status: Accepted
Date: 2026-10-18
Decided by: Phase 4 fix loop 1 (go-implementer), recorded for the Phase 4 round 2 reviewers

## Context

Phase 4 round 1 reviews (`docs/development/reviews/phase-4-foundry-lead-round1.md`,
`phase-4-azure-black-belt-round1.md`, both REJECT) found that `assess waf`:

- selected its controls with a hard-coded group allow-list and had a completeness test that could not fail;
- reported partly skipped evaluations and any recorded question answer as PASS;
- turned informational posture findings into WARNING;
- marked controls "costly" from an unsourced Go list and `pillar == cost`;
- did not say that PSRule, Azure Advisor and Defender for Cloud evidence was not considered;
- shipped no rendered sample reports.

This ADR records the decisions taken in fix loop 1, and the items that are deferred rather than fixed.

## Decision

1. **Versioned mapping file.** `internal/assess/waf/mapping.yaml` (version 1, reviewed 2026-10-18) lists every catalogue rule exactly
   once, as `mapped` (pillar must equal the catalogue pillar) or `excluded` (with a reason). `waf.Validate` and
   `mapping_test.go` fail when a catalogue rule is missing, duplicated, mapped to another pillar, or when a rule scheduled in phases 1-4
   is excluded. All 78 live phase 1-4 rules are mapped, including CFG, ENV, DEP and RUN. The 30 exclusions are rules scheduled only
   for later/future phases, the phase 7 cost-estimate rule, and phase 8 Foundry IQ / AI gateway rules (their IQ and gateway metadata
   is not loaded by `assess waf`).
2. **Pillars and checklist codes.** Pillar keys come from the catalogue `pillar` field and are printed with the WAF pillar names
   (https://learn.microsoft.com/azure/well-architected/pillars, read 2026-10-18). **Deferred:** mapping each rule to a WAF checklist
   code (RE:, SE:, OE:, CO:, PE:) and citing a WAF service guide per pillar assignment. Control IDs are Foundry Doctor rule IDs; every
   report says so in its limitations.
3. **States.** Added `INFO` (evaluated; only informational findings at this profile's severity), `ATTESTED` (a team
   answer or delegation recorded in configuration, not verified) and `NOT_APPLICABLE`. An evaluated rule with a skip
   that hid a check is `UNKNOWN` ("Partial evaluation"), never `PASS`. Every control carries `basis`
   (`deterministic`, `attested`, `none`) and a `stateReason`.
   **Amended in fix loop 2 (round 2 NB1):** `assess` no longer derives applicability from `rules.ClassifySkip`, which
   exists to decide `--strict` exit codes (ADR-010 decision 9) and stays unchanged. `assess.classifySkip` has its own
   explicit classification:
   - `policy-managed` and `central-dns` (configuration delegates the setting to Azure Policy or central DNS) →
     `ATTESTED`, basis `attested`, reason "Delegated to Azure Policy by configuration; assignment and compliance not
     verified". Of the two treatments the reviewers offered (ATTESTED, or QUESTION for must-have controls) we chose
     ATTESTED for all, **plus**: a mandatory control in `ATTESTED` keeps the decision at "Proceed with conditions", and
     it is listed (marked mandatory) under "Team-attested answers" in the owner view. Deployed policy evidence is
     not read offline (item 7).
   - "Nothing declared" heads (`no-*-declared`, `no-*-requirement`, for example FND-REL-009
     `no-resilience-objective-declared` and FND-OPS-002 `no-explicit-observability-or-server-tracing-requirement`)
     → `QUESTION`, listed in the unassessed scope (and, when flagged, in the costly section).
   - Only "the project has no such resource or scope" skips (`not-applicable:`, `dev-profile`,
     `environment-not-classified-as-dev`, `project-has-no-agent-services`, the gateway "no API" heads and the
     `input-unavailable` details "project has no capability host", "account has no model deployments", ...) →
     `NOT_APPLICABLE`. "No dependency is proven private-endpoint backed" is *not* treated as absent.
   - Everything else (not-implemented, synthetic-infrastructure, profile-key-missing, input-unavailable, rule
     errors, unknown heads) hides the check: `SKIPPED`, or `UNKNOWN` when the rule evaluated other checks.
   `INFO`, `NOT_APPLICABLE` and non-mandatory `ATTESTED` never enter the top actions or force the decision; the
   advisory LLM projection excludes PASS, INFO, ATTESTED and NOT_APPLICABLE (round 2 NB2).
4. **Questions.** Prompts for FND-IDN-005, FND-OPS-002, FND-OPS-008, FND-REL-007 and FND-REL-009 are in `mapping.yaml`.
   Only FND-REL-007 has configuration keys (ADR-007): `disasterRecovery.declared` must be boolean `true` and
   `disasterRecovery.reference` a non-empty string for `ATTESTED`; `false` is `WARNING` (attested); any other value or a missing
   reference stays `QUESTION`. **Deferred:** configuration keys for FND-IDN-005, FND-OPS-002, FND-OPS-008 and FND-REL-009 (FND-REL-009 reads
   `reliability.apim*Required`, which the configuration schema does not define). ADR-007 defines none, and adding keys
   is a configuration-schema change outside Phase 4. Until then these controls are answered only by deterministic evidence, or stay
   `SKIPPED`/`QUESTION` with their prompt shown.
5. **Costly recommendations.** The cost flag lives in `mapping.yaml` with a basis, a primary source and a verification date. Only
   non-mandatory rules may be flagged (enforced by `Validate`). `pillar == cost` no longer implies costly: Cost Management budgets
   (FND-COST-001) are free and FND-COST-002 reduces spend. Costly optional controls that are outstanding are rendered in a separate
   "Costly recommendations (optional)" section in the owner, HTML and evidence views and are excluded from the top five actions.
   Flagged on 2026-10-18 from Microsoft Learn: FND-NET-010 (loop 2: the Azure Monitor private link article refers its
   cost to Azure Private Link pricing), FND-NET-011, FND-OPS-002, FND-OPS-006, FND-REL-001, FND-REL-002, FND-REL-003
   (basis narrowed to continuous backup; zone-redundancy cost not cited), FND-SEC-008, FND-SEC-010, FND-SEC-011.
   **Not flagged (UNVERIFIED):** FND-OPS-003 (metric alert billing), FND-REL-008 (spillover traffic billing) and
   FND-REL-009 (the API Management feature comparison shows zones and multi-region only on Premium tiers, but we
   found no Learn statement of the cost difference; the rule also recommends the *least* sufficient tier). FND-OPS-001 is must-have; its ingestion cost
   is real (cost-logs article) but a mandatory control is not an optional costly recommendation.
6. **External evidence is not ingested.** PSRule for Azure (adapter deferred by ADR-020), Azure Advisor and Microsoft Defender for
   Cloud recommendations are **not imported** by `assess waf` in this release. Every report states this in its limitations, and
   the developer view lists the catalogue `overlap` PSRule and Azure Policy IDs as "overlapping external checks (not ingested)".
   Importing them as advisory evidence with provenance (PRD section 12 integration table) is deferred to a later release.
7. **Deployed evidence.** `assess waf` runs offline. Phase 2 and 3 controls (DEP, RUN, IDN-005, SEC-008/009, NET-006, OPS-009's
   deployed part) are mapped but are `SKIPPED` unless their rules evaluate offline; the limitations list names each one that was
   not evaluated. Adding subscription/environment flags to run the deployed adapters from `assess waf` is deferred.
8. **Sample reports.** (Loop 2: each Markdown and HTML sample carries an in-file banner; JSON cannot carry one
   without changing the pack schema, so `evidence.json` relies on the directory marker and README.) `samples/assess-{dev,test,prod}/reports/` hold owner, developer and evidence reports in every format. In
   fix loop 1 no Go toolchain was available, so they were **hand-derived** (a port of the assess and renderer code over an
   *estimated* engine outcome). `samples/assess-*/reports/HAND-DERIVED` marks this. `TestAssessSampleReports`
   (`cmd/foundry-doctor/assess_samples_test.go`) renders the samples with the real catalogue and engine and **fails** while the
   marker exists, until the maintainer regenerates with `-update-assess-samples`, reviews and commits.
9. **Decision wording and ranking** (`No-go`, `Proceed with conditions`, rank weights) are project opinion and say so in every
   report's limitations.

## Consequences

- PRD section 12 DoD items stay unmet until the Phase 4 round 2 reviews close and the sample reports are regenerated with Go.
- A catalogue rule added without a `mapping.yaml` entry fails `go test ./internal/assess/waf`.
- Follow-ups: WAF checklist codes and service-guide citations; config keys for IDN-005/OPS-008; PSRule/Advisor/Defender import;
  deployed-evidence mode for `assess waf`; verify the three unflagged cost candidates.
