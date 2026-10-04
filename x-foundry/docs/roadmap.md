# Roadmap notes by phase

Decisions and ideas recorded during Phase 1 so they are picked up in the right phase. Nothing
here is implemented yet. Scope for each item is a starting point to revisit when the phase begins.

## Rule for Well-Architected (WAF) features

Features are grouped by the effort to support them natively (see `waf-profiles.md` for the current
recommendations):

- **Low effort: native.** The setting already exists in the schema or is a small addition to a
  resource the extension creates. The extension generates it and, in `prod`, recommends it.
- **Medium and high effort: comments only.** The extension does not generate these. The `doctor`
  command (Phase 6) adds a one-line description or pseudo-Bicep as a comment in
  `Azure-WAF-recommended.yaml`, and users write their own Bicep.

| Group | Examples | Handling |
| --- | --- | --- |
| Low | Zone redundancy, Cosmos continuous backup, ZRS storage, delete locks, Key Vault purge protection, Defender plans, Azure Policy assignments, budgets, alerts, diagnostic settings | Native |
| Medium | Customer-managed keys, Azure Monitor private link scope, per-resource diagnostic extras | Comment in recommended YAML |
| High | Azure Firewall, UDR and forced tunnelling, NSGs, MCP subnet, DDoS protection, Application Gateway or Front Door with WAF, provisioned deployments with spillover, multi-region failover and DR, per-environment subscription isolation | Comment in recommended YAML |

## Phase 2: Bicep generator

- Generate the low-effort WAF resources: resource locks, zone-redundant and ZRS settings, Cosmos
  continuous backup, Key Vault purge protection, diagnostic settings to Log Analytics.
- Name generation uses `internal/azurenames`.
- Pseudo-Bicep for the medium and high groups is written here as reference material for `doctor`
  (Phase 6). It is unverified until it can be tested against Azure, and the comments must say so.

## Phase 5: gateway and governance

- Generate the governance-related low-effort items: Azure Policy assignments, Defender plans,
  budgets and alerts.

## Phase 6: azd integration and developer experience

### `xfoundry doctor`

- Runs the `prod` profile against `azure.yaml` and writes `Azure-WAF-recommended.yaml`. The
  original file is not changed.
- Uses `yaml.Node` from `go.yaml.in/yaml/v3` so key order and the user's comments are preserved.
- For each `XF3xx` warning:
  - If the key exists, the comment goes above it.
  - If the key is missing, the comment goes above the nearest existing parent, with the suggested
    YAML commented out.
- For gaps with no schema field (medium and high groups), the comment is a one-line description or
  pseudo-Bicep the user can customise.
- Comments are prefixed (for example `# WAF[Reliability] XF303:`) so they are distinguishable from
  user comments and the command is idempotent.
- Tests: golden files per example, and a check that the annotated copy still parses and validates.
- Needs only Phase 1 output, so it can be pulled forward if wanted.

### Suggest test and prod after dev

After a successful dev deployment, the extension suggests creating `test` and then `prod`
environments so the SDLC follows the Well-Architected golden path. See `xfoundry promote` below.

### `xfoundry promote` (environment migration)

Creates the YAML for the next environment from an existing one. This works on YAML only and
does not need deployed state.

- `xfoundry promote --from dev --to test`, and `--from test --to prod`. The source file is
  never modified.
- Sets `defaults.environment` and, in the new file, adds the target profile's recommendations as
  comments (reusing `doctor`).
- Carries over portable settings: topology, models and deployments, agents, toolboxes, MCPs,
  knowledge bases, projects, roles, evaluation, gateway endpoints and quotas, tags.
- Does not carry over, or asks about: names and `namingPrefix`, region, existing resource IDs
  (VNet, subnets, Search, Storage), IP rules, secrets references, budgets, runtime image and scale,
  and anything that identifies a specific deployment.
- Interactive by default: it walks through each top-level section with what will be copied and
  lets the user include, exclude or edit. `--yes` accepts the defaults; `--include` and
  `--exclude` give the same control in scripts.
- Decision: each environment has its own file (for example `azure.dev.yaml`, `azure.test.yaml`,
  `azure.prod.yaml`). There is no `environments` overlay in the schema, so no schema change or
  merge rule is needed. The cost is that files can drift apart after promotion; Phase 6 should
  decide how azd selects the file per environment and whether a `diff` between environment
  files is worth adding.
