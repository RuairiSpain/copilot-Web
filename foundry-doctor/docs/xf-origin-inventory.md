# XF-origin inventory and provenance gate

Last reviewed: 2026-10-05

## Status

The PRD states that there are 59 XF-origin checks, but the repository does not contain the XF
source inventory, stable XF identifiers, source revision, licence/provenance manifest, or a
check-to-rule mapping. A repository-wide search found only the PRD's count. Consequently no XF
check can be attributed to an FND rule without inventing provenance.

This inventory is deliberately fail-closed:

- `source`: **unavailable** means no XF source artefact was supplied.
- `mapping`: **unresolved** means no FND rule mapping is asserted.
- `decision`: **deferred** means no reuse/adapt/drop decision is approved.
- None of these rows grants permission to copy code, tests, wording, or metadata.
- Replace the local ordinal with the source's stable identifier only when the source manifest is
  supplied. Changing an FND ID or phase still requires the ADR required by PRD section 20.

## Complete 59-check placeholder inventory

| Local inventory key | Source revision | Source path/check ID | FND mapping | Decision | Gate |
|---|---|---|---|---|---|
| XF-ORIGIN-001 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-002 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-003 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-004 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-005 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-006 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-007 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-008 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-009 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-010 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-011 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-012 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-013 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-014 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-015 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-016 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-017 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-018 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-019 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-020 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-021 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-022 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-023 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-024 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-025 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-026 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-027 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-028 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-029 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-030 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-031 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-032 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-033 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-034 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-035 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-036 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-037 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-038 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-039 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-040 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-041 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-042 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-043 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-044 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-045 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-046 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-047 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-048 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-049 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-050 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-051 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-052 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-053 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-054 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-055 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-056 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-057 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-058 | unavailable | unavailable | unresolved | deferred | blocked |
| XF-ORIGIN-059 | unavailable | unavailable | unresolved | deferred | blocked |

## Evidence required to unblock

Supply all of the following together: immutable source revision, paths and stable check IDs, the 59
check definitions and tests, licence and redistribution terms, and the prior mapping if one exists.
The catalogue owner can then record exact provenance and make per-check decisions. Until then the
Phase 0 XF provenance gate remains blocked.
