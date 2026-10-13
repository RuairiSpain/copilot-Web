# Architecture

## Pipeline (weekly, Thursday 18:00 UTC)

| Step | Module | Input | Output |
|---|---|---|---|
| 1. Scan | `tracker/scan.py` | Source repos in `config/sources.yml`, watermark in `state/state.json` | `data/<repo>/commits/*.csv`, `data/<repo>/changes/*.csv`, `data/scans.csv`, updated watermark |
| 2. Summarise | `tracker/summarise.py` | Change CSVs | `data/<repo>/summaries/<week>.json` |
| 3. Build site | `tracker/build_site.py` | Change CSVs and summaries | `site/data/meta.json`, `changes.json`, `summaries.json` |
| 4. Check URLs | `tracker/check_urls.py` | Change CSVs | Log only |
| 5. Publish | GitHub Actions | `site/` | GitHub Pages |

## Sync model
- **Watermark:** each repo stores `last_sha`. The next run reads `last_sha..HEAD`.
- **Fallback:** if the watermark commit is missing from the shallow clone, the run reads commits since `last_sync`.
- **Safe to re-run:** rows are de-duplicated on (commit sha, file path).
- **Weeks:** rows are bucketed by commit date (Monday start). A week's summary is regenerated when its row count changes.

## Classification
- Front-matter-only edits → `metadata-only`, importance 0.
- Keyword rules on changed lines and the commit message → tags (breaking-change, retirement, ga, preview, security, pricing-quota, models, region, api-sdk).
- Importance = change-type weight + tag weights + size, capped at 100.

## Roadmap
- **v1 POC:** azure-ai-docs only.
- **v1:** all Cloud & AI Platforms repos (design doc Appendix A1) plus the Azure Updates feed.
- **v2:** Microsoft 365, Security and Business Applications sources.
