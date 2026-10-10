# Annotate

`foundry-doctor annotate` turns findings into source-linked review artefacts.
It never edits deployable originals.

## Modes

- `--format review` copies `azure.yaml` and the `infra/` tree into a
  repo-relative output directory whose name must end in `.review`, inserts
  tool-owned comments, writes `annotations-manifest.json`, and optionally
  writes `annotations.diff` with `--diff`.
- `--format github` writes GitHub Actions workflow-command annotations to
  stdout or `--out`.
- `--format sarif` writes SARIF 2.1.0 annotations to stdout or `--out`.

## Examples

```text
foundry-doctor annotate --profile test --out review\sample.review
foundry-doctor annotate --profile prod --out review\sample.review --diff
foundry-doctor annotate --format github --min-severity warning
foundry-doctor annotate --format sarif --out annotate.sarif
```

## Safety and limitations

- Originals remain unchanged; only the review copy is written.
- Output paths must stay inside the project directory.
- Review directories must end in `.review` to reduce accidental deployment.
- Annotation text is redacted and escaped so it cannot inject GitHub workflow
  commands (`%`, newlines, and `::` are neutralised).
- Findings without precise source locations stay in `annotations-manifest.json`
  and are not inlined.
- Bicep inline comments are limited to exact compiler diagnostics. ARM-derived
  Bicep findings remain manifest-only until source mapping is proven.
- Review-mode Bicep validation currently requires a single Bicep entry module;
  projects using `infra.layers` return unavailable until layered validation is
  implemented.
