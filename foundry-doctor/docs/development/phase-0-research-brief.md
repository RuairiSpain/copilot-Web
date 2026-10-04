# Phase 0 research brief

Shared instructions for the agents that research the rule catalogue. Read `CLAUDE.md` and
`.claude/rules/foundry-doctor-rule-catalog.md` first.

## Source access in this environment

`learn.microsoft.com`, `api.github.com` and `vuln.go.dev` are blocked by the egress proxy.
`raw.githubusercontent.com`, `github.com` (clone and release download), the Go proxy, NuGet and npm work.
The Learn articles are built from public GitHub repositories, so verify against those and
record the public Learn URL as `sources[].url`, with the file you actually read in `verifiedVia`.

Local shallow, sparse clones are in `REFS` (path given in your task). Read them with Grep and Read.

| Directory | Repository | Use |
|---|---|---|
| `psrule/` | Azure/PSRule.Rules.Azure (`docs/en/rules`, `src`) | Existing rule coverage, WAF pillar tags, rule logic |
| `azure-dev/` | Azure/azure-dev (`cli/azd/docs`, `cli/azd/extensions`, `schemas`) | azd commands, extension framework, azure.yaml schemas |
| `azd-docs/` | MicrosoftDocs/azure-dev-docs | azd docs (Learn source) |
| `azure-ai-docs/` | MicrosoftDocs/azure-ai-docs (`articles/foundry`) | Foundry docs (Learn source), azure.yaml reference |
| `azure-docs/` | MicrosoftDocs/azure-docs (selected service folders) | Search, APIM, Defender, Private Link, DNS, RBAC, Key Vault, Cosmos, Monitor, Advisor |
| `rest-api-specs/` | Azure/azure-rest-api-specs (selected services) | Authoritative ARM property names, enums, API versions |
| `azure-policy/` | Azure/azure-policy (`built-in-policies`) | Built-in policy definitions |
| (none) | MicrosoftDocs/well-architected is not public | WAF pillar guidance: use PSRule's WAF tags and `wara/`; cite the public Learn WAF URL only if a clone of the source shows it |
| `wara/` | Azure/Azure-Proactive-Resiliency-Library-v2 | WARA recommendations |
| `checkov/`, `kics/` | bridgecrewio/checkov, Checkmarx/kics | ARM/Bicep policy coverage |
| `bicep/` | Azure/bicep (`docs`) | Bicep linter rule docs |

If a clone is missing or a fact is not in these, you may fetch other raw GitHub files.
If a fact cannot be verified from a primary source, do not guess: set the rule to
`product-opinion` if it is a design preference, or leave it `proposed` and list it under
"Unverified" in your fragment with what you tried.

## What to produce per rule

Edit the rule's existing seed file in `rules/catalog/<group>/<ID>.yaml`. Keep `id`, `group`, `title`
(you may tighten the wording, not change the meaning), and `phases`. Fill the schema in
`internal/catalog/catalog.go` (`Rule`), as in `.claude/skills/add-foundry-rule/rule-template.yaml`:

- `status`: `verified` (platform fact confirmed), `product-opinion` (no platform source; basis must contain
  `opinion`), or `dropped` (overlap decision `drop`, with reason). Leave `proposed` only for the unverifiable.
- `description`, `inputs`, `basis`, `category`, `pillar`, `severity` (dev/test/prod), `compatibility`
  (azd/extension/API versions you verified against), `evidence`, `recommendation`, `fix` (short safe example),
  `sources` (URL plus `lastVerified: "2026-10-04"` plus `verifiedVia`), `testability`, `tests` (scenario names).
- `overlap`: `decision` (reuse | wrap | adapt | native | drop), `coverage` (none | partial | full),
  `rationale`, and the mapped IDs in `psrule`, `azurePolicy`, `defender`, `advisor`, `bicepLinter`, `checkov`.
  Use real IDs you found; write `[]` when none exists. State the evidence difference when coverage is partial.
- `implementation`: `owner` (`native` unless the decision is reuse/wrap/adapt) and a proposed `package`.
- Platform-basis rules are `error` in every profile. Do not mark a rule `platform` unless a primary source
  states the platform constraint.
- Rules whose phase is later than 1 still need a decision and a source; Phase 8 rules touching preview
  schemas should say which version the property names were verified against, or stay `proposed` with a note.

Decision guide: `reuse` = the existing tool already gives equivalent evidence, Foundry Doctor only maps its ID;
`wrap` = run the tool and normalise its output; `adapt` = take the idea, express it with Foundry correlation;
`native` = no equivalent, or Foundry-specific correlation is the value; `drop` = low value or unverifiable.
Do not copy PSRule implementation code.

Implementation owner: `native` for `native` and `adapt` (our code); an adapter/tool owner (`psrule`, `azure-policy`, `defender`, `advisor`, `bicep`, `checkov`, `adapter`) only for `reuse` and `wrap`.

## Fragment

Write `docs/overlap/<your-groups>.md` (lowercase, e.g. `cfg-env.md`) with: a table of every rule in your groups
(ID, decision, coverage, mapped external IDs, status), a short list of rules you changed to `dropped` or
`product-opinion` and why, an "Unverified" list, and up to 3 proposed NEW rules per group that would help
developers or product managers (deployment feasibility, supportability, schema lifecycle, release readiness,
cost visibility). Proposed rules go in the fragment only; do not create catalogue files for them.

## Constraints

- Edit only the catalogue directories and the fragment named in your task. Other agents work in parallel.
- Run `go run ./cmd/rulecatalog validate` from `foundry-doctor/` before reporting. Fix every error in your groups.
  Errors in other groups are not yours.
- Never state a property name, enum value, SKU limit or API version that you did not read in a source.
- Report: counts by status and decision, anything you could not verify, and any PRD ambiguity.
