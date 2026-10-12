# Profiles

A profile decides how severe each rule's finding is. It is the only thing a profile does. It holds no organisation-specific
value (those are `policy:` keys, see [config-reference.md](config-reference.md)), and it does not choose which rules run
(that is `rules:` and `--rules`).

| Name in the CLI | Long name | Intended use |
|---|---|---|
| `dev` (default) | `foundry-dev` | Local work and early adoption. Fewest errors. |
| `test` | `foundry-test` | Shared test environments. |
| `prod` | `foundry-prod` | Production. Most errors. |

`--profile` and the `profile:` config key accept either form. Any other value exits 2.
Reports print the long name. If neither the flag nor the file names a profile, the profile is `foundry-dev`. The PRD names no
default; `dev` was chosen as the least disruptive for adoption ([ADR-010](../decisions/ADR-010-command-surface.md), decision 6).
The report header shows the profile that was used.

## How severities differ

Severity per rule and profile is part of each rule's catalogue entry. `foundry-doctor explain <rule-id>` prints it, for example
`Severity: foundry-dev=info, foundry-test=warning, foundry-prod=warning` for FND-OPS-007.

Of the 38 rules in the Phase 1 pack `foundry-core`, counted from `rules/profiles/*.yaml`:

| Profile | error | warning | info |
|---|---|---|---|
| `foundry-dev` | 10 | 20 | 8 |
| `foundry-test` | 27 | 9 | 2 |
| `foundry-prod` | 33 | 3 | 2 |

27 of the 38 rules have a different severity in at least one profile.

Rules whose basis is `platform` (a limit or constraint of the Azure or Foundry platform, not a recommendation) are errors in
every profile. The rule code applies this before it looks at the profile (`SeverityFor` in `internal/rules/executor.go`),
and `TestPlatformBasisAlwaysError` checks the profile files.

A policy key can lower a finding's severity but never raise it. Today only `policy.network.publicAccess` does this, for
FND-NET-001 ([ADR-007](../decisions/ADR-007-profile-configuration-keys.md)), and never for a platform-basis rule.

Engine diagnostics (`FND-SYS-*`) have a fixed severity in every profile.

## Where the profile files come from

`rules/profiles/foundry-dev.yaml`, `foundry-test.yaml` and `foundry-prod.yaml` are generated from the rule catalogue
(`go test ./internal/config -run TestGenerateProfiles -update`). Do not edit them by hand: `TestProfilesUpToDate` and
`TestProfileSeveritiesMatchCatalogue` fail when they differ from the catalogue. They list all 108 catalogued rules, including rules
that do not run in Phase 1. In this build the severity used at run time is read from the catalogue entry, and the tests keep
the profile files equal to it.

## Choosing a profile

- Use `--profile` for a single run, or `profile:` in `.foundry-doctor/config.yaml` for the project default.
- Use `--fail-on` to choose which severity fails the build, and `--min-severity` to choose what the report lists. A profile
  changes severities; these two flags change what you see and when the exit code is 1.
- Switching to a stricter profile can turn existing warnings into errors. Write a [baseline](baseline-and-suppressions.md)
  first if you need to adopt gradually. Baseline entries survive a profile change because the fingerprint does not include the
  profile or severity.
- A profile does not make skipped checks run. Rules skipped for a missing policy key stay skipped in every profile.
