# Sample WAF assessment reports

Rendered for every audience and format: `owner.md`, `owner.html`, `developer.md`, `developer.html`,
`evidence.md`, `evidence.json`.

**Status: hand-derived, not engine output.** Fix loop 1 had no Go toolchain. The files were produced by a
line-by-line port of `internal/assess` and the renderers, fed with an *estimated* rule-engine outcome
(which rules evaluate, skip or report findings offline). Skip reasons marked "estimated", finding wording
and the `(regenerate)` fingerprints are placeholders. Each Markdown and HTML file starts with a
hand-derived banner; `evidence.json` has none because the pack schema has no comment field. See ADR-026.

`cmd/foundry-doctor/assess_samples_test.go` (`TestAssessSampleReports`) renders this sample with the real
catalogue, mapping and engine (no Bicep CLI, clock fixed at 2026-10-18T00:00:00Z) and fails while
`HAND-DERIVED` exists. Regenerate with:

```text
go test ./cmd/foundry-doctor -run TestAssessSampleReports -update-assess-samples
```

Review the diff, then commit. The update deletes `HAND-DERIVED`; from then on the test is a golden test.
