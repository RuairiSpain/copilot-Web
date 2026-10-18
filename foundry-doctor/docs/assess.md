# WAF assessment

`foundry-doctor assess waf` converts Foundry Doctor findings into three WAF-oriented views:

- **owner**: decision, state counts, top five actions, a separate list of costly optional recommendations,
  team-attested answers, unassessed scope with reasons, and limitations (Markdown or HTML)
- **developer**: per-control evidence basis, state reason, source and verification date or project-opinion label,
  WARA references, overlapping external checks, recommendation, fix and finding locations (Markdown or HTML)
- **evidence**: auditable Markdown or JSON pack that keeps passed, failed, suppressed (with reason, owner and expiry),
  baselined, attested, not-applicable and skipped controls visible, with tool version, generation time, rule versions
  and fingerprints

## Scope and limitations

- The report maps available Foundry Doctor evidence to selected WAF-aligned controls. It does **not** claim complete
  Azure Well-Architected compliance and is not a certification.
- `assess waf` runs **offline**. Deployed Azure state is not queried; deployment (phase 2) and runtime (phase 3)
  controls that were not evaluated are named in the report limitations.
- **PSRule for Azure, Azure Advisor and Microsoft Defender for Cloud recommendations are not imported** in this
  release (ADR-020, ADR-026). The developer view lists the catalogue's overlapping PSRule and Azure Policy IDs for
  reference only.
- Control IDs are Foundry Doctor rule IDs. WAF checklist codes (RE:, SE:, OE:, CO:, PE:) are not mapped yet (ADR-026).
- The decision wording ("No-go", "Proceed with conditions", ...) and the top-action ranking are **project opinion**.

## Usage

```text
foundry-doctor assess waf [--profile dev|test|prod] [--dir <path>]
                          [--baseline <file>] [--suppressions <file>] [--strict]
                          [--audience owner|developer|evidence]
                          [--format markdown|html|json]
                          [--evidence-pack]
                          [--out <path>]
                          [--llm-explain [--llm-provider <name>] [--llm-timeout <1s..30s>]]
```

- `--format html` is available for `owner` and `developer` (two different pages); `--format json` only for the evidence pack.
- `--evidence-pack` overrides `--audience`.
- `--strict` exits 3 when a skip hid a check. By-design skips (not-applicable, not-implemented, synthetic
  infrastructure, missing profile key) do not count, but they stay visible as SKIPPED, NOT_APPLICABLE, ATTESTED or QUESTION.
- `--llm-explain` appends an advisory narrative to owner or developer output (explicit opt-in; never for the
  evidence pack). It never changes a state. `--llm-provider` and `--llm-timeout` apply only with `--llm-explain`.

Examples:

```text
foundry-doctor assess waf --profile prod
foundry-doctor assess waf --audience developer --format html --out assess.html
foundry-doctor assess waf --evidence-pack --format json --out evidence.json
```

## States

| State | Basis | Meaning |
|---|---|---|
| PASS | deterministic | The rule evaluated every check and no finding remains |
| FAIL | deterministic | An active **error**-severity finding (for the selected profile) remains, mandatory or not |
| WARNING | deterministic or attested | An active warning finding, a suppressed/baselined finding, or a team answer of `false` |
| INFO | deterministic | Evaluated; only informational findings at this profile's severity (for example CMK or egress posture, or a must-have rule whose `dev` severity is info). See the evidence; not an action |
| ATTESTED | attested | The team recorded an affirmative answer, or delegated the setting to Azure Policy or central DNS (`managedByAzurePolicy`, `network.dnsManagedByPolicy`, `network.centralDns`). Foundry Doctor did not verify it |
| UNKNOWN | deterministic or none | Partial evaluation (some checks skipped) or no evidence. Never PASS |
| QUESTION | none | The control needs a business or operational answer, or a requirement/objective is not declared (for example FND-OPS-002, FND-REL-009) |
| SKIPPED | none | The rule did not run (reason shown) |
| NOT_APPLICABLE | deterministic | The project has no resource or scope the rule checks (for example no Cosmos DB account, or the rule does not apply to the dev profile) |

`assess` classifies skip reasons itself (ADR-026 item 3); it does not reuse the `--strict` classification.

Decision: any mandatory FAIL is "No-go"; a mandatory WARNING, ATTESTED, UNKNOWN, QUESTION or SKIPPED is "Proceed
with conditions"; any other FAIL, WARNING, UNKNOWN or QUESTION is "Proceed with recommendations". INFO,
NOT_APPLICABLE, PASS and non-mandatory ATTESTED never affect the decision; none of them is a top action.

### Questions

| Rule | Prompt | Configuration answer |
|---|---|---|
| FND-REL-007 | DR strategy recorded with a reference | `policy.disasterRecovery.declared: true` **and** a non-empty `policy.disasterRecovery.reference` gives ATTESTED; `declared: false` gives WARNING; anything else stays QUESTION |
| FND-IDN-005 | Standing human admin access reviewed | No configuration key (ADR-007); answered only by deterministic evidence |
| FND-OPS-008 | Promotion gated by evaluations | No configuration key (ADR-007); answered only by deterministic evidence |
| FND-OPS-002 | Telemetry requirement declared | No configuration key; the rule always reports "no requirement declared", so it stays QUESTION |
| FND-REL-009 | APIM SLA, zone or region objective | The rule reads `reliability.apim*Required`, not yet in the configuration schema; stays QUESTION (ADR-026) |

The prompts are project opinion.

## Mapping file

`internal/assess/waf/mapping.yaml` (versioned, reviewed 2026-10-18) lists every catalogue rule as `mapped` (with
its catalogue pillar) or `excluded` (with a reason), plus the question and costly entries. Pillars are printed with
the WAF pillar names (https://learn.microsoft.com/azure/well-architected/pillars). `go test ./internal/assess/waf`
fails when a rule is missing, duplicated, mapped to a different pillar, or when a phase 1-4 rule is excluded.

## Costly recommendations

A control is costly only if `mapping.yaml` says so with a basis, a Microsoft Learn source and a verification date.
Mandatory rules are never flagged, and the cost pillar does not imply costly (budgets are free; FND-COST-002 saves
money). Outstanding costly optional controls appear in their own "Costly recommendations (optional)" section and
not in the top five actions. See ADR-026 for the list and the unverified candidates.

## Sources

Each control shows the first catalogue source URL and its `lastVerified` date. Controls from product-opinion rules
are labelled "Project opinion, not Microsoft guidance" in every view, with the source shown as context. WARA
references show the full APRL GUID, a label and the `pgVerified` flag; WARA is a recommendation set, not a platform fact.

## Sample fixtures and reports

- `samples/assess-dev/` (synthetic infrastructure)
- `samples/assess-test/` (unresolved `uses:` reference and missing variable producer)
- `samples/assess-prod/` (Bicep infrastructure and a DR declaration with a reference)

Each has `reports/` with owner, developer and evidence output in every format. **These reports are engine output**
(generated without the Bicep CLI, so template-dependent controls are SKIPPED). Regenerate them with
`go test ./cmd/foundry-doctor -run TestAssessSampleReports -update-assess-samples` (ADR-026).
