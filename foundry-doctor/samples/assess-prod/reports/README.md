# Sample WAF assessment reports

Rendered for every audience and format: `owner.md`, `owner.html`, `developer.md`, `developer.html`,
`evidence.md`, `evidence.json`.

**Status: engine output.** Regenerated with `-update-assess-samples` on Go 1.26.9 after Phase 4 loop 1.

`cmd/foundry-doctor/assess_samples_test.go` (`TestAssessSampleReports`) renders this sample with the real
catalogue, mapping and engine (no Bicep CLI, azd version injected as 1.34.2, clock fixed at 2026-10-18T00:00:00Z)
and compares the output with these files. Because no Bicep CLI is present, template-dependent controls show as
SKIPPED. A run on a machine without `azd` on PATH reports the azd-version-gated controls as skipped as well.
Regenerate with:

```text
go test ./cmd/foundry-doctor -run TestAssessSampleReports -update-assess-samples
```

Review the diff, then commit.
