# Phase 1 re-verification: Foundry lead (heads c5f3fa4, then b920094)

Reviewer: `foundry-lead` (independent, read-only). Date: 2026-10-16.

First pass at c5f3fa4: **ACCEPT with conditions.** N3-1 and N3-2 fixed (CFG finding plus uncertain note; CFG-009 camelCase, `schedule` and no-cron triggers give `uncertain`). Open: N4-1 (`parseAzdRange` reported an error for `>=1.0.0 <`, `>1.0.0 <`, `> = 1.0.0`, repeated `||`, which blang/semver v4.0.0 `ParseRange` accepts), CFG-009 list-shaped `triggers:` and singular `trigger:` read as not-applicable, N4-2 generic remediation, N4-3 stale ADR-004 wording, N4-4 `--rules` exit-2 behaviour undocumented.

Second pass at b920094 (fixes in 007b195): **ACCEPT.** The five strings now give `uncertain`; `>=1.0.0 ||` and `|| >=1.0.0` stay invalid; list and singular trigger shapes give `uncertain`; remediation text and ADR-004 updated; exit-2 behaviour documented in `--help` and README. No open findings. Observation: some library-invalid strings (`<`, `>=`, `*`, `1.0.0-next`) are reported as unverified, which is safe.

Not verified by the reviewer: ENV behaviour with real `.env` files (tool guard blocked creating them; unit tests only), routines key names `cronExpression`/`timeZone` against a primary source, godotenv parity, `AZURE_ENV_NAME` export, `.azure/config.json` default environment semantics.
