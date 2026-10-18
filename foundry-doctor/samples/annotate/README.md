# Annotate samples

These examples illustrate Phase 6 annotation outputs.

- `review-manifest.example.json` shows the manifest shape written by
  `foundry-doctor annotate --format review`. The copied `azure.yaml` is
  written as `azure.review.yaml`; every copied `azure.yaml` and `.bicep` file
  is listed, with `annotationCount` 0 when it only carries the banner.
- `github-workflow.example.txt` shows GitHub workflow-command annotations for
  CI. Escaping is applied once, at this sink: `%` becomes `%25`; the message
  and the recommendation are joined with ` — `.
