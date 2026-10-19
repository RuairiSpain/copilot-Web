# ADR-029: Open-ended azd compatibility (azd 1.34.2 and later)

Status: Accepted
Date: 2026-10-19
Decided by: the project owner's request ("make the azd rules compatible with versions of azd 1.34.2 onwards")

## Context

44 catalogue rules declared `compatibility.azd` as a closed range, `>=1.34.2 <=1.36.0` (41 rules) or
`>=1.36.0-beta.1 <=1.36.0` (3 rules). The upper bound was the newest azd release whose schemas and source had been read. The
engine skips a rule whose azd version is outside the range (`unsupported-version`), so every new azd release disabled 32 to 44
rules until someone re-read the release and widened the ranges. On azd 1.37.0 the 10 OPS rules, SEC-014 and others skipped
although they read only compiled ARM or policy keys.

## Decision

1. `compatibility.azd` accepts an open-ended range. `>=1.34.2` (legacy scalar) or `{minimum: 1.34.2, includeMinimum: true}`
   with no `maximum` accepts every later version. A `maximum` without a `minimum`, and `includeMaximum` without a
   `maximum`, stay invalid.
2. The 44 closed ranges were changed to `>=1.34.2`. The three that began at `1.36.0-beta.1` now begin at 1.34.2 too; a rule
   whose input exists only in newer azure.yaml schemas must test for the input, not for the azd version (SEC-007 hosted-agent
   leg: see the rule's catalogue notes).
3. The newest verified azd version is the constant `catalog.AzdVerifiedThrough` (1.36.0). When the azd version in use is
   newer, the engine adds a warning to the run diagnostics (stderr): rules ran on a version they were not verified against. The
   report body and exit codes are unchanged.
4. `preflight` and `runtime` accept `--azd-version` like `doctor`, so the DEP and RUN rules can be version-gated when azd is
   not on PATH.
5. Extension ranges (`compatibility.extensions`) stay closed; the engine does not gate on them today.

## Consequences

- A new azd release no longer disables rules. The cost is that a breaking azd change is not detected by the version gate; the
  diagnostic tells the operator the version is unverified, and the maintainers still widen `AzdVerifiedThrough` and re-read the
  release notes per release.
- A lower bound still skips azd versions older than 1.34.2 as `unsupported-version`.
