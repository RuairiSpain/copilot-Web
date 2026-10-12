# Adapters (Phase 1): PSRule for Azure

`internal/adapters` holds shared helpers; `internal/adapters/psrule` is the optional PSRule for Azure adapter
(PRD sections 6 and 9, ADR-001, ADR-009). PSRule is an overlap verifier, not a core dependency, and PowerShell is
never bundled. Native findings win: the app passes adapter findings through `engine.DedupAdapter`.

## Behaviour

- **Detect** (never errors): `disabled` (`advanced.psrule.enabled: false`, no process started), `missing` (`pwsh` not
  on PATH, resolved to a non-absolute path, or module not installed), `unsupported-version` (PowerShell below 7.4),
  `failed` (probe crashed, timed out, or printed unexpected output), `available`. It runs
  `pwsh -NoProfile -NonInteractive -Command <constant>`; the constant is in `psrule.go` and has no interpolation.
- **Run** only when enabled and available. Inputs are compiled ARM templates passed in `AdapterRequest.Options`
  as `arm:<logical file name>` = ARM JSON text. The adapter writes them to a fresh `os.MkdirTemp` directory
  (`input/NNNN.json` plus a linking `NNNN.parameters.json`, and `ps-rule.yaml` enabling
  `AZURE_PARAMETER_FILE_EXPANSION`), runs a fixed `Assert-PSRule ... -OutputFormat Sarif` command with that directory as
  working directory, reads `out/results.sarif` (size-limited), and deletes the directory. It never reads the project tree.
- **Environment**: allow-list only (`PATH`, `HOME`, `PSModulePath`, `XDG_*`, Windows system variables, `LANG`), plus
  telemetry opt-outs and `TMPDIR/TMP/TEMP` pointing at the work directory. No Azure, GitHub or other credentials.
- **Limits**: detect 30 s, run 5 min, 8 MiB per stream and per results file, 10 000 results, 256 templates of at most 16 MiB.
- **Errors** (all wrap a sentinel in `internal/adapters`): `ErrDisabled`, `ErrUnavailable` (mode decides exit 2 vs
  skip), `ErrTimeout`, `ErrOutputTooLarge`, `ErrProtocol` (malformed SARIF, wrong version, invalid rule name,
  non-zero exit, missing results file). Everything except disabled/unavailable maps to exit 4.
- **Parser**: strict SARIF 2.1.0 subset (`version`, `runs[].results[]` with `ruleId`, `level`, `kind`, `message.text`,
  `locations[].physicalLocation.artifactLocation.uri`, `locations[].logicalLocations[].name|fullyQualifiedName`).
  Rule names must match `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`, otherwise the whole run is a protocol failure.
  Messages and names are stripped of control and bidi characters, `<`, `>` and backticks are replaced, text is
  redacted with `internal/findings` and only then truncated. Results for one rule on one resource are merged into one
  finding (messages sorted, at most five) so a rule never yields two fingerprints.
- **Mapping**: PSRule rule name -> catalogue rules via `overlap.psrule` (`engine.OverlapIndex`, passed as `Config.Mapper`).
  One finding per mapped rule, `Adapter="psrule"`, `Key` = PSRule rule name, `Confidence=likely`, Severity/Category left
  empty for the engine to stamp. Unmapped rules become `psrule/<RuleName>` (RuleVersion 1, `nice-to-have`, severity hint from
  SARIF level: error/warning/note -> error/warning/info, `likely`). Resource and `/resources/N` pointer are resolved by
  matching the reported logical name against the supplied template (exact, unique match); otherwise the reported name is kept
  without type and pointer, so `DedupAdapter` cannot absorb it.

## Version policy

- PowerShell 7.4 minimum: verified (PSRule for Azure `docs/install.md`).
- PSRule.Rules.Azure 1.47.0 is only a proposal (`docs/tool-compatibility.md` 2.4). It is **not enforced**:
  `Config.MinModule` is empty by default and Detect reports the version only. Set `MinModule: psrule.ProposedMinModule` to enforce.

## Unverified (needs a live spike, item 4 of the tool-compatibility open items)

No `pwsh` exists in the authoring environment, so nothing here was run against PSRule. Not verified from primary sources:

1. The exact SARIF PSRule writes: which fields it fills (`logicalLocations`, `artifactLocation.uri`, `kind`), and whether
   passing results are included. The parser reads only standard SARIF fields and fails closed on anything else. The PSRule
   JSON record format (`-OutputFormat Json`) was not verifiable from the PSRule.Rules.Azure repository (the engine source
   is a different repository), so JSON output is not parsed.
2. That `-Option 'ps-rule.yaml'` and parameter-file expansion (documented in `docs/using-templates.md`) work for
   templates with required parameters; compiled Bicep output with defaults is the assumed case.
3. That the logical name PSRule reports equals the ARM resource `name` after expansion. Templates whose names are
   expressions (`[parameters('x')]`) will not resolve to a type, which only means no de-duplication, never a wrong merge.
4. That `Get-Module -ListAvailable` finds a user-scope install under the restricted environment (HOME and PSModulePath are kept for this).
5. Exit-code behaviour of `Assert-PSRule -ErrorAction Continue` on failing rules; the script ends with an explicit `exit 0`
   when the results file exists and `exit 3` when it does not.

## Wiring for `internal/app`

```go
mode, _ := adapters.ParseMode(cfg.Advanced.PSRule.Enabled)    // "auto" | "true" | "false"
idx := engine.NewOverlapIndex(catalogRules)
ad := psrule.New(psrule.Config{Mode: mode, Mapper: idx})      // production defaults: ExecRunner, exec.LookPath, os.Environ
st := ad.Detect(ctx)                                          // goes into report.tools (engine redacts Path to base name)
if u := ad.Availability(st); u != nil {
    // u.Fatal (mode true): exit 2. Otherwise emit FND-SYS-ADAPTER-UNAVAILABLE with u.Evidence(); skip dependent checks.
} else {
    opts := map[string]string{psrule.InputPrefix + logicalName: compiledARMJSON} // one per compiled template
    fs, err := ad.Run(ctx, sdk.AdapterRequest{Profile: profile, Options: opts})
    // err: errors.Is(ErrDisabled/ErrUnavailable) -> as above; anything else -> exit 4
    // then: stamp fs (catalogue fields, findings.Sanitize, Fingerprint), then
    // kept, absorbed := engine.DedupAdapter(native, fs, idx)
}
```
