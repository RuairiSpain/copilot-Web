# Phase 1 re-verification: Azure black belt (c5f3fa4 to 7299567)

Reviewer: `azure-black-belt` (independent, read-only; Bicep CLI 0.48.1, `--azd-version 1.36.0`). Date: 2026-10-16.

| Head | Verdict | Findings |
|---|---|---|
| c5f3fa4 | ACCEPT with conditions | P1, P1b, P3 (root variables), P5-P8 fixed; P1c, P4 evidence text, child-module variables recorded as limitations. New: N1 (`/* */` between `loadTextContent` and `(` leaked an outside file), N2 (outside `.json` module), N3 (URL string ending in `.bicep` refused the entry), N4 (`equals`/`contains` treated as case-insensitive; Microsoft documents case-sensitive), N5-N7 minor |
| b920094 | ACCEPT with conditions | N1-N7 fixed. New: M1 (module reference with a non-`.bicep` extension), M2 (symlink to `.azure`), M3 (`.git`), M4 (`bicepconfig.json` above the project) |
| f3b8fe8 | ACCEPT with conditions | M1-M3 fixed, M4 recorded. New: S1 (module through a symlinked directory pointing outside the project), S2 (ADR wording) |
| 7299567 | **ACCEPT** | S1, S2 fixed. Ten further symlink and path bypass attempts refused. No blocking finding |

ADR-018 amendment 4 states that the guard is a best-effort lexical scan and lists what it does not examine. Not tested: Windows junctions; a `readEnvironmentVariable` project that compiles (informational).
