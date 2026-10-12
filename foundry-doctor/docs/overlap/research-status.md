# Overlap research-state contract

Last reviewed: 2026-10-05

This file is the per-tool research-state contract for every rule in `rules/catalog/`. The strict
catalogue schema records six mappings under `overlap.research`: `psrule`, `azurePolicy`, `defender`,
`advisor`, `bicepLinter` and `checkov`. Each mapping has a state and, for `searched-match`, the exact
matched IDs. `overlap.provisional` is true whenever any configured tool remains unresearched.

## Meaning of catalogue values

- **searched-match** means the mapping contains the exact IDs already verified for that rule. It does
  not claim that an exhaustive search of the tool's catalogue was completed.
- **searched-none** may be claimed only when the rule rationale or its group fragment names the
  catalogue and immutable revision (or a dated/released catalogue version) searched and says no
  match was found. A working-tree path, branch name, ad hoc text search or unavailable revision is
  insufficient.
- **unresearched** means the source was unavailable or was not searched deeply enough. This is the
  migration default for every legacy empty tool array unless the preceding searched-none evidence
  exists.
- `overlap.decision` is **provisional** whenever any configured tool is unresearched. It becomes a
  final implementation decision only after the configured-tool searches and the two required expert
  reviews.
- The same rule applies to tools without a first-class mapping (including WARA, KICS and Terrascan):
  a mapped ID is searched-match; a revisioned exhaustive search may be searched-none; otherwise
  the tool is unresearched.

## Current state by tool

| Tool | State | Scope/revision evidence | Consequence |
|---|---|---|---|
| PSRule for Azure | 27 searched-match; 0 searched-none; 81 unresearched | Local `refs/psrule` paths named in group fragments; immutable commit unavailable | Existing IDs are mapped; unmatched rules remain unresearched |
| Azure Policy built-ins | 33 searched-match; 0 searched-none; 75 unresearched | Local built-in definition paths/GUIDs named in fragments; immutable catalogue revision unavailable | Existing GUID matches are usable; unmatched rules remain unresearched |
| Bicep linter | 4 searched-match; 0 searched-none; 104 unresearched | Microsoft Learn linter pages/local Bicep material named in fragments | Existing rule-code matches are usable; unmatched rules remain unresearched |
| Checkov | 15 searched-match; 0 searched-none; 93 unresearched | Local check IDs named in fragments; immutable commit unavailable | IDs are provisional to the reviewed clone; unmatched rules remain unresearched |
| Defender for Cloud | 3 searched-match; 0 searched-none; 105 unresearched | No complete recommendation catalogue/revision was available | Unmatched rules remain unresearched |
| Azure Advisor | 0 searched-match; 0 searched-none; 108 unresearched | No complete recommendation catalogue/revision was available | Every rule remains unresearched |
| WARA | searched-match only for listed `aprlGuid` values in rule sources/fragments; otherwise unresearched | Local working tree/HEAD was used; no immutable revision was recorded | Matches are context; no searched-none claim is permitted |
| KICS, Terrascan and other configured third-party tools | unresearched | No configured, revisioned source packet was supplied | No searched-none claim is permitted |

## Group decision state

All catalogue decisions and group-fragment decisions are provisional research recommendations, not
final approvals. CFG/ENV, OPS/COST, NET/IDN, DEP, IQ/GW, REL, RUN and SEC each have at least one
required tool that is unresearched under the contract above. This qualification overrides wording
such as "none" or "no equivalent" when that wording is not accompanied by a named, revisioned
exhaustive search. A later revisioned search may promote one tool to searched-none;
it does not finalize the decision while another required tool remains unresearched.

## Source revision policy

`lastVerified` records when content was read. `verifiedVia` records the exact local path or public
URL and, where available, the page `ms.date`, API version, release version or immutable revision.
A branch name such as `main` is not an immutable revision. Where the local reference snapshot has
no recorded commit, this catalogue must say **revision unavailable** rather than manufacture a
hash. Refreshing a source changes `lastVerified`; it does not silently widen compatibility.

## Catalogue-wide test semantics

The Go catalogue contract applies the same meaning to all 108 rule records:

- `tests.positive` is an input that produces the rule's violation/finding. For informational rules,
  this means a deterministic informational finding, not a compliant report.
- `tests.negative` is compliant input and produces no finding.
- `tests.skipped` names the unavailable input, unsupported version or missing capability that
  prevents evaluation.
- `tests.uncertain` means evaluation ran but the available evidence cannot deterministically support
  either a finding or compliance.

Scenario names do not redefine these outcomes. In particular, successful output-generation cases
belong under `negative`; `positive` is reserved for malformed, incomplete or non-compliant output
that raises a finding.
