# ADR-009: Engine diagnostics namespace

Status: Accepted
Date: 2026-10-05

## Context

The engine must report problems that are not catalogue rules: an expired suppression (PRD section 9: "Expired suppressions generate an error"), an
unparseable azure.yaml, an unsupported host, an unavailable adapter, and Bicep compiler diagnostics (PRD section 9: "Normalise diagnostics into Finding
records"). The catalogue validator requires every rule ID to match `FND-<GROUP>-<NNN>` and the catalogue has no group for these.
Giving them catalogue IDs would mean rules with no sources, tests or overlap decisions. Giving them ad hoc IDs would let fingerprints and baselines collide.

## Decision

1. **Reserved IDs outside the catalogue.** Engine diagnostics use `FND-SYS-<NAME>` (upper-case, hyphenated). Compiler diagnostics use `bicep/<code>`, where
   `<code>` is the compiler's own code such as `BCP035` or `no-hardcoded-env-urls`. Neither form is a catalogue rule.
2. **The catalogue validator keeps rejecting them.** `FND-SYS` is not an accepted catalogue group, so a catalogue file for one fails the existing ID check. The reserved
   names live in Go (`internal/engine`), not in `rules/catalog/`. `foundry-doctor explain FND-SYS-*` prints the built-in text.
3. **Fixed severities, not profile-dependent**, because they describe the run, not the project's posture:

   | ID | Severity | Meaning |
   |---|---|---|
   | `FND-SYS-SUPPRESSION-EXPIRED` | error | A suppression's expiry date has passed. The suppressed finding becomes visible again. |
   | `FND-SYS-SUPPRESSION-INVALID` | error | A suppression lacks a reason or expiry, or is malformed. |
   | `FND-SYS-INVALID-AZURE-YAML` | error | azure.yaml failed to parse or violates the schema; dependent rules are skipped. |
   | `FND-SYS-UNSUPPORTED-HOST` | warning | A service host the engine does not know; rules for it are skipped. |
   | `FND-SYS-ADAPTER-UNAVAILABLE` | info | An optional adapter is missing or disabled; dependent checks are skipped (ADR-001). |
   | `FND-SYS-DUPLICATE-FINGERPRINT` | warning | Two distinct findings collapsed to one fingerprint (ADR-008). |
   | `bicep/<code>` | from the compiler: error to error, warning to warning, info to info | Compiler or linter diagnostic. Confidence is `likely` when the location is derived. |

4. Engine diagnostics are ordinary `Finding` records (so they flow through console, JSON, Markdown and SARIF), with `Category` empty and `Basis` empty.
   They take part in `--fail-on` but cannot be baselined: baselining a broken suppression file would hide the breakage. They can be suppressed only if
   the diagnostic is not itself about suppressions.
5. SARIF maps `bicep/<code>` to rule ID `bicep/<code>` and engine IDs to themselves; none appears in the rules index of the catalogue documentation.

## Consequences

- Expired suppressions meet the Definition of Done without adding a fake rule to the catalogue.
- Adding a `FND-SYS-*` ID is an ADR amendment; a test fails when code emits an unlisted `FND-SYS-*` ID.
- Fixed severities mean a `--fail-on warning` run also fails on `FND-SYS-UNSUPPORTED-HOST`; that is intended.
