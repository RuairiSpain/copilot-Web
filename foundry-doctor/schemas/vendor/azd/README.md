# Vendored azd schemas

Offline copies of the azd `azure.yaml` JSON schema and the `azure.ai.*` extension schemas, used by `internal/azureyaml`
(rule FND-CFG-001, ADR-011). Nothing here is loaded from the network at run time.

| Files | Source | Ref |
|---|---|---|
| `azure.yaml.json` | `Azure/azure-dev` `schemas/v1.0/azure.yaml.json` | tag `azure-dev-cli_1.35.0` = commit `170ebb858071da353cc9bf8657a377bff268ba36` (2026-09-30) |
| `azure.ai.agents/`, `azure.ai.connections/`, `azure.ai.projects/`, `azure.ai.routines/`, `azure.ai.skills/`, `azure.ai.toolboxes/`, `azure.ai.evaluations/` | `Azure/azure-dev` `cli/azd/extensions/<ext>/schemas/*.json` | commit `a64ca8fe04d0bc1b0375c32fed1b76573e2a91ef` (2026-09-30), carried by tags `azd-ext-azure-ai-agents_1.0.0-beta.18`, `-connections_1.0.0-beta.9`, `-toolboxes_1.0.0-beta.9`, `-projects_1.0.0-beta.13` |

Notes:

- `azure.ai.evaluations/azure.ai.eval.json` is unreleased at that commit (its only tag, `azd-ext-azure-ai-evaluations_1.0.0-beta.1`,
  is an older commit and differs). `azure.ai.skills` and `azure.ai.routines` carry beta.8 tags on the same commit.
- Only `$id` and string `$ref` values that were absolute `raw.githubusercontent.com/Azure/azure-dev/main/...` URLs were changed.
  They now point at the sibling copies, relative to this directory (for example `azure.ai.agents/azure.ai.agent.json`).
  Everything else is byte-identical to the source. Regenerate with `scripts/ci/vendor-azd-schemas.sh <azure-dev clone>`.
- `SHA256SUMS` lists the hash of every file here. `internal/azureyaml` has a test that fails when an embedded file no longer
  matches the hashes recorded in the test source, so a refresh must update the commits above, `SHA256SUMS` and that test together.
- `schemas/embed_azd.go` (`schemas.AzdFS()`) embeds these files. Go cannot import a package below a directory named
  `vendor`, and `./...` skips such directories, so the embed lives one level up and the drift test in `internal/azureyaml`.

## Licence

`Azure/azure-dev` is MIT licensed, Copyright 2022 (c) Microsoft Corporation. The schema files carry no per-file header.
The licence text, copied from the repository root at the same commit, is in `LICENSE` in this directory.
