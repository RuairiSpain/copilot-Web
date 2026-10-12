# Baseline and suppressions

Two mechanisms let an existing project adopt Foundry Doctor without fixing everything first. They differ:

| | Baseline | Suppression |
|---|---|---|
| Purpose | "Accept what exists today." | "Accept this finding on purpose, for now." |
| Matches by | Exact fingerprint only. | Exact fingerprint, or a resource selector for one rule. |
| Needs a reason, owner, expiry | No. | Yes, all three. |
| Expires | No. | Yes. After its expiry date the finding is visible again and an error is reported. |
| Created by | `foundry-doctor doctor --write-baseline <file>` | You, by hand. |
| Schema | [schemas/baseline.schema.json](../../schemas/baseline.schema.json) | [schemas/suppressions.schema.json](../../schemas/suppressions.schema.json) |
| Example | [schemas/examples/baseline.example.yaml](../../schemas/examples/baseline.example.yaml) | [schemas/examples/suppressions.example.yaml](../../schemas/examples/suppressions.example.yaml) |

Neither hides a new or changed finding. Engine diagnostics (`FND-SYS-*`, `bicep/*`) cannot be baselined, so a broken
suppression file cannot be hidden by a baseline.

## Workflow

1. Run once on the project and write a baseline:

   ```
   foundry-doctor doctor --write-baseline .foundry-doctor/baseline.yaml
   ```

   The run treats the written findings as accepted, so it can exit 0 even when findings are at or above `--fail-on`.
   Commit the file.

2. Later runs read `.foundry-doctor/baseline.yaml` automatically (the `adoption.baseline` default). Use `--baseline <file>` for
   another path. A finding whose fingerprint is in the file is marked `baselined`, is not counted towards the exit code, and
   shows in the summary as `baselined`. A new finding, or one whose fingerprint changed, fails the run as before.

3. To accept one finding for a limited time, add an entry to `.foundry-doctor/suppressions.yaml` (or `--suppressions <file>`).

4. To refresh the baseline after fixing findings, run `--write-baseline` again. It writes the findings that are live in that run
   (not suppressed), so fixed findings drop out of the file. This build does not report baseline entries that no longer match
   any finding, and it does not report suppressions that match nothing; prune them yourself.

Relative paths for `--baseline`, `--suppressions` and `--write-baseline` are relative to the project root. They must stay
inside it, and symbolic links on the path are refused.

A missing file at the default path is normal. A missing file that you named with `--baseline` or `--suppressions` gives exit 2.

## What a fingerprint covers

`fp1:` plus 32 hex characters, computed from the rule ID, the rule version, the logical resource (kind, lower-cased type,
name), the finding's key and a stable evidence key ([ADR-008](../decisions/ADR-008-finding-fingerprint.md)). Line numbers,
file paths, profile, severity and evidence text are not part of it. Consequences:

- Moving or reformatting code, or changing the profile, does not change the fingerprint.
- Renaming a resource, or a new rule version, changes it. The old baseline entry then stops matching and the finding comes back.
- Environment findings use key names, never values.

Find the fingerprint of a finding in the console report (`fingerprint:` line) or the JSON report.

## Baseline file format

```yaml
version: 1
generatedBy: foundry-doctor dev
entries:
  - fingerprint: fp1:3b4beeaee71c8873a8bf9d95996dd695
    ruleId: FND-OPS-007
    ruleVersion: 1
    resource:
      kind: project
      name: arm-json
    firstSeen: "2026-10-12"
```

This file was produced by running `--write-baseline` on a copy of `test/fixtures/project/arm-json` with the prod profile.

Rules for the file: only `version: 1`; unknown keys are an error; `fingerprint` must match `fp1:` plus 32 lower-case hex digits and be
unique; `ruleId` must not be a `FND-SYS-*` or `bicep/*` ID; `ruleVersion` is at least 1; `firstSeen`, when present, is a real
`YYYY-MM-DD` date. The resource block holds a logical identity and never an Azure resource ID. Size limit 4 MiB. A single
YAML document only. An invalid baseline file is exit 2 (the run does not continue without it).

## Suppressions file format

```yaml
version: 1
suppressions:
  # Suppress one exact finding.
  - ruleId: FND-OPS-007
    fingerprint: fp1:3b4beeaee71c8873a8bf9d95996dd695
    reason: Pipeline is managed in another repository.
    owner: platform-team
    expires: 2026-12-31
  # Suppress a rule on every resource that matches the selector.
  - ruleId: FND-COST-002
    resource:
      kind: arm-resource
      type: microsoft.cognitiveservices/accounts
      name: dev-account
    reason: Dev-only resource, tracked in the team backlog.
    owner: dev-lead
    expires: 2027-03-31
```

Rules for the file:

- Each entry needs `ruleId`, `reason`, `owner`, `expires` and exactly one of `fingerprint` or `resource`.
- `resource` has any of `kind`, `type`, `name`, `pointer`; every field you set must match. `type` is compared without regard to case.
- `reason` and `owner` must not be blank. `expires` must be a real `YYYY-MM-DD` date.
- `ruleId` may not be `FND-SYS-SUPPRESSION-*`. Two entries with the same rule and target are an error.
- When several valid entries match, the first in file order is used.
- Unknown keys, more than one YAML document, and files over 1 MiB are rejected.
- The `reason` text is redacted before it is shown. Do not put secrets in it anyway.

### Expiry rule

A suppression is valid through the end of its expiry day, in UTC. With `expires: 2026-12-31` it still hides the finding at
`2026-12-31T23:59:59Z` and stops hiding it at `2027-01-01T00:00:00Z`. The calendar day is always taken in UTC, whatever your
time zone. After that:

- the finding is visible again, with its normal severity;
- an error finding `FND-SYS-SUPPRESSION-EXPIRED` is reported for the entry (certain, severity `error` in every profile), so the
  run exits 1 at the default `--fail-on error`.

Example output, from a run with an entry that expired on 2020-01-01:

```
FND-SYS-SUPPRESSION-EXPIRED [certain]
  evidence: suppression of FND-OPS-007 (owner platform-team) expired on 2020-01-01 (valid through the end of that day, UTC); the finding is visible again
  recommendation: Fix the finding, or renew the suppression with a new expiry date and a current reason.
```

### An invalid suppressions file

If the file cannot be used (a missing reason, owner or expiry, a bad date, an unknown key), no suppression from it is applied
and the run reports `FND-SYS-SUPPRESSION-INVALID` (error) listing the problems found. The run is not exit 2 in this
case; it is exit 1 because of that error finding. Fix the file.

## What neither mechanism does

- A baseline never covers `FND-SYS-*` or `bicep/*` diagnostics, and a suppression never covers `FND-SYS-SUPPRESSION-*`.
- They do not change `--min-severity`; a covered finding below the display level is simply not listed.
- A skipped check is not a finding, so there is nothing to baseline or suppress for it.
- Neither file is signed or access controlled. Review changes to them as you review code.
