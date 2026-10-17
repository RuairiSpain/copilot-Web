# foundry-rules

The Foundry Doctor rule catalogue (108 rules, `FND-<GROUP>-<NNN>`) as a plain PSRule rule set. No Go code, no CLI,
no azd extension, no reporter, no engine. Rule IDs, titles, severities per profile, rationale, recommendation and
documentation URLs are taken from `foundry-doctor/rules/catalog/` (read only). Nothing under `foundry-doctor/` depends
on this folder, and this folder does not depend on it at run time.

## Status: PSRule fixture check passes in CI

PSRule runs this rule set in `.github/workflows/foundry-rules.yml` (Ubuntu and Windows, PowerShell Gallery install of
`PSRule.Rules.Azure` 1.47.0, which installed from the Gallery). On 2026-10-17 the fixture check
(`tests/Invoke-FixtureCheck.ps1`) passed on both runners for all 10 fixtures: every "Must fail" and "Must not fail"
assertion in `tests/expected.md` held, and the not-evaluated placeholders produced no result. The first run failed
for the ARM JSON fixtures because PSRule bound objects by .NET type name and matched no rule; `ps-rule.yaml` now sets
`binding.targetType: [type]` and `targetName: [name]`, which fixed it.

What the check does not cover: Bicep input (`-Format File` with Bicep expansion), `.bicepparam`, resources PSRule for
Azure expands from real templates (the ARM fixtures are hand-written), the `FOUNDRY_REQUIRED_TAGS` configuration path
(FND-OPS-004 is skipped when no tags are set), and PSRule versions other than the one the workflow installs. Treat
rules outside the 28 evaluated in part as documentation, not as checks.

Verified so far:

- `tools/verify.py` (static, Python + PyYAML; also run by the workflow): all 108 catalogue IDs appear exactly once in
  the rule files (100 in `Foundry.Rule.yaml`, 8 in `Foundry.Rule.ps1`), in `docs/rule-index.md`, in `.ps-rule/en/` and
  in each of the three baselines; titles and dev/test/prod severities equal the catalogue; every YAML file and JSON
  fixture parses; every property path in a rule appears in the evidence of the same catalogue entry.
- PowerShell parse check under PowerShell 7.5.0 on Linux (`[System.Management.Automation.Language.Parser]::ParseFile`):
  `.ps-rule/Foundry.Rule.ps1` and `tests/Invoke-FixtureCheck.ps1` have no syntax errors.
- The 8 PowerShell rule bodies were also executed outside PSRule (with a stub for `Rule`) against the
  `azure.*.yaml` and `arm.*.json` fixtures. Their pass/fail values matched `tests/expected.md` for the resources they
  apply to. This does not test PSRule's own binding, `-Type`/`-If` handling, baselines or `$Configuration`.
- All 100 YAML rule documents and the three baselines validate against the PSRule language JSON schema
  (`schemas/PSRule-language.schema.json` on PSRule `main`, fetched 2026-10-17), and `ps-rule.yaml` against
  `PSRule-options.schema.json`. That check found `input.format` is not an option key; it was removed (the `-Format`
  parameter, set to `Detect` in the fixture script, picks the format). A schema check does not prove the rules behave
  as intended, and `main` may be newer than the PSRule version that PSRule for Azure 1.47.0 loads.

Not verified: that PSRule loads these files, that any YAML selector evaluates as intended, that the baselines apply
(`override.level`, `rule.tag`), that the `Foundry.NotEvaluated` placeholders stay silent, that Bicep expansion works,
and every item under "PSRule facts not verified" below. The CI job runs `tests/Invoke-FixtureCheck.ps1`, which asserts
`tests/expected.md`; a failure there means either a rule or an expectation is wrong, and neither is to be adjusted
without checking the rule definition.

## The one dependency

[PSRule for Azure](https://github.com/Azure/PSRule.Rules.Azure) (`PSRule.Rules.Azure`). It brings PSRule and the
Bicep expansion. For `.bicep` input PSRule for Azure itself uses the Bicep CLI; you do not install or call it
separately for this rule set. No other module or package is used. The generator in `tools/` is a maintainer tool
(Python 3 with PyYAML), not a dependency: generated files are committed and users never need Python.

## Install and run

```powershell
Install-Module PSRule.Rules.Azure -Scope CurrentUser
cd foundry-rules

# compiled ARM JSON or azure.yaml
Invoke-PSRule -InputPath ../my-project/azure.yaml,../my-project/infra/main.json -Baseline Foundry.Prod -Module PSRule.Rules.Azure

# Bicep (expanded by PSRule for Azure)
Invoke-PSRule -InputPath ../my-project/infra/main.bicep -Format File -Baseline Foundry.Prod -Module PSRule.Rules.Azure
```

Use `Foundry.Dev`, `Foundry.Test` or `Foundry.Prod`. Always pass a baseline: it sets the level of every rule, including
the PowerShell rules. `ps-rule.yaml` limits the run to rules tagged `ruleSet: foundry-rules`, so the `Azure.*` rules
from PSRule for Azure do not run.

## Layout

| Path | Content |
|---|---|
| `ps-rule.yaml` | PSRule options: module, Bicep expansion, rule filter, `FOUNDRY_REQUIRED_TAGS` |
| `.ps-rule/Foundry.Rule.yaml` | 100 YAML rules: 20 `static-yaml` selectors and 80 `not-evaluated` placeholders |
| `.ps-rule/Foundry.Rule.ps1` | 8 PowerShell rules (`static-ps`), all single-object |
| `.ps-rule/en/FND-*.md` | one page per rule: synopsis, description, recommendation, example, notes, links |
| `baselines/{dev,test,prod}.Rule.yaml` | `Foundry.Dev`, `Foundry.Test`, `Foundry.Prod`: level per rule |
| `docs/rule-index.md` | all 108 rules: ID, title, severity per profile, how evaluated, status |
| `docs/not-evaluated.md` | the 80 rules PSRule does not evaluate, with the reason |
| `tests/` | 10 fixtures, `expected.md` and `Invoke-FixtureCheck.ps1` (asserts `expected.md`; run by CI, no Pester) |
| `tools/` | `generate.py`, `verify.py`, `rulespec.py`, `rulelogic.py`: maintainer tools |

"One rule file per rule" is implemented as one rule definition per rule: PSRule loads all definitions from the files
in `.ps-rule/`, and each ID is defined exactly once.

## Evaluation classes

| Class | Rules | Meaning |
|---|---|---|
| `static-yaml` | 20 | PSRule YAML rule: a property comparison on one ARM resource |
| `static-ps` | 8 | PowerShell rule over one object (azure.yaml, or an ARM resource with an array to iterate) |
| `needs-live` | 40 | Evidence comes from the control plane, a data plane, a runtime probe or a cost API. PSRule cannot evaluate it offline |
| `needs-input` | 40 | Evidence needs input PSRule does not receive as one object: `.env`, pipeline files, API Management policy XML, JSON schemas, profile declarations, or several correlated resources |

A rule that is not evaluated (80 of 108) is still defined, so it appears in the index and the rule docs. Its YAML
definition has `type: Foundry.NotEvaluated`, which no input object has, so it never runs and can never be reported as
passed. Skipped is not passed.

An evaluated rule is an `evaluated-subset`: PSRule evaluates only the part of the catalogue evidence that is a property
comparison on one object. The part that is left out is stated on each rule page under "Evaluated here" and in
`docs/rule-index.md`. Where the catalogue scopes a rule by correlation (for example "accounts that own a project"), the
offline rule is broader than the catalogue rule and can report more resources. Where the catalogue says
"uncertain" or "skipped", the offline rule produces no result for that case; PSRule has no third outcome.

## How each rule maps to PSRule

- `static-yaml`: `kind: Rule` with `spec.type` (the ARM resource types), optional `spec.where` (a precondition; also
  used to skip values that are still unresolved ARM expressions), and `spec.condition`.
- `static-ps`: `Rule 'FND-...' -Type ... -If { ... } { ... }` returning a boolean. azure.yaml services are a map keyed by
  service name, which YAML selectors cannot iterate, so those rules are PowerShell.
- Overlap with PSRule for Azure: the catalogue lists related `Azure.*` rules for some rules (for example
  FND-SEC-001 and `Azure.AI.DisableLocalAuth`). Each rule page lists them under "Related PSRule for Azure rules". They are
  related, not proven equivalent, and are not called by this rule set.

## Severity

The catalogue has `error`, `warning` and `info` per profile. PSRule levels: `error` = `Error`, `warning` = `Warning`,
`info` = `Information`. Each YAML rule carries the prod severity as its default `spec.level`. PowerShell rules have no
level parameter, so their level comes only from the baseline. The three baselines list every rule once and set its level
(`spec.override.level`). Platform-basis rules are `error` in every profile in the catalogue; that is unchanged.

`FND-COST-002` and `FND-SEC-010`/`FND-SEC-011` need a note. COST-002 is meant for dev environments; the catalogue sets
warning in dev and info in test and prod, so the offline rule still reports on test and prod resources, at Information.
SEC-010 and SEC-011 are informational in the catalogue (no fail state); here a Fail at Information means "not configured".

## What was not carried over

- **Evidence kinds PSRule cannot hold:** see `docs/not-evaluated.md`. Rules marked `Needs a resource-set wrapper object`
  would need all resources of a template in one JSON object. That wrapper is not built.
- **Rule metadata:** rule version, phases, input planes, basis, category, pillar, compatibility ranges (azd, extensions,
  API versions), `lastVerified` per rule, implementation owner and the positive/negative/skipped/uncertain test lists are
  not copied. The rule page keeps all source URLs with their `lastVerified` dates, the catalogue status
  (`verified` or `product-opinion`) and a flag when the catalogue notes contain unverified items. Product-opinion rules
  are labelled as such on their page and in the index.
- **Uncertain results, fingerprints, suppressions, profile policies (for example `policy.network.publicAccess`):**
  PSRule options and suppression are used instead where needed; none are configured here.
- **Configuration-driven rules** (required tags, residency, allow/deny lists, retention, resilience objective): only
  FND-OPS-004 reads configuration (`FOUNDRY_REQUIRED_TAGS`); the others are `needs-input`.

## PSRule facts not verified

Written from the PSRule documentation but not confirmed against a running PSRule: the `spec.where` and `spec.level`
keys in YAML rules (seen in PSRule for Azure rules and the testing guide, not in the expressions page); `kind: Baseline`
with `spec.override.level` (documented in the options page; the PSRule version that introduced it was not found);
the `-Format Detect` default (the former `input.format` key was removed, see Status); the PowerShell `-If`, `-Tag` and `$Configuration` use; that functions and `$Configuration` are
available in a `-If` precondition; how an expanded Bicep resource names child resources (`account/project`); and the
behaviour of `notEquals`, `startsWith` and `notIn` on a missing field (the rules add explicit `exists: false` branches
for the first). Mapping of PSRule outcomes (Fail, Pass, None) to "skipped" is by PSRule behaviour for rules whose type
or precondition does not match.

## Regenerating (maintainers only)

```
python3 foundry-rules/tools/generate.py   # rewrites generated files from foundry-doctor/rules/catalog
python3 foundry-rules/tools/verify.py     # static checks listed above
```

Edit `tools/rulespec.py` (class and subset per rule) and `tools/rulelogic.py` (the PSRule logic), never the generated
files. `tests/` and this README are hand-written.
