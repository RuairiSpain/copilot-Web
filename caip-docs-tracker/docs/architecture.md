# Architecture

## Pipelines

Weekly scan (Thursday 18:00 UTC, `.github/workflows/caip-docs-tracker-weekly-scan.yml`):

| Step | Module | Input | Output |
|---|---|---|---|
| 1. Announcements | `tracker/announcements.py` | `feeds` in `config/sources.yml`, `state/feeds.json` | `data/announcements/<week>.csv` |
| 2. Scan | `tracker/scan.py` | Enabled sources (`sources.yml` + `discovered.yml`), `state/state.json` | `data/<repo>/{changes,commits}/*.csv`, `data/scans.csv`, watermark |
| 3. Summarise | `tracker/summarise.py` | Change CSVs | `data/<repo>/summaries/<week>.json` |
| 4. Build site | `tracker/build_site.py` + `correlate.py` + `store.py` | Changes, summaries, announcements | `site/data/*.json`, `site/feeds/*.xml` |
| 5. Check URLs | `tracker/check_urls.py` | Change CSVs | Log only |
| 6. Digests (after the data commit) | `tracker/digest.py --send`, then the digest record is committed with retries | Changes, summaries, announcements, `config/subscriptions.yml`, `state/digests.json` | Teams / email, `data/digests/<date>/*` |
| 7. Publish | GitHub Actions | `site/` | GitHub Pages |

Discovery (Monday 06:00 UTC, `caip-docs-tracker-discovery.yml`): `tracker/discover.py` → `data/discovery.json`,
`config/discovered.yml`. Both workflows share one concurrency group because both commit to `main`.

## Components (design document, section 4)

| Design component | Implementation |
|---|---|
| Source registry | `config/sources.yml`; `config/discovered.yml` kept current by `discover.py` |
| Collector | `announcements.py` (feeds, conditional requests) and `scan.py` (git, watermark per repository) |
| Noise filter | `scan.classify`: metadata-only, whitespace-only, link-only, typo-sized and redirection-file changes are noise |
| Classifier and summariser | `scan.classify` labels every change; `summarise.py` batches per product and week |
| Correlator | `correlate.py`: same product, ±14 days; unmatched significant changes are silent |
| Store | CSV/JSON in the repository (see README, "Deviations from the design") |
| Delivery | `digest.py` (Teams webhook, Logic App, SMTP), per-product RSS, the dashboard |

## Sync model
- **Watermark:** each repo stores `last_sha`. The next run reads `last_sha..HEAD`.
- **Fallback:** if the watermark commit is missing from the shallow clone, the run reads commits since `last_sync`.
- **Safe to re-run:** change rows are de-duplicated on (commit sha, file path), announcements on (feed id, item id).
- **Pull requests:** a branch commit takes the PR number of the `Merge pull request #N` commit that first brought it in.
- **Failures:** a failed source or feed is logged and its watermark is left unchanged; the others still run, and the
  step exits non-zero at the end. A failed digest leaves that subscriber's "last sent" time unchanged.
- **Weeks:** rows are bucketed by commit date in UTC (Monday start). A week's summary is regenerated when its row
  count changes, or on the next run if the model was unavailable.

## Classification
- Tags from keyword rules on changed lines (and the commit message for the first four): breaking-change, retirement,
  ga, preview, security, pricing-quota, models, region, api-sdk; plus structural tags (new-page, removed-page,
  moved-page, navigation, include, whats-new, code-sample).
- Importance = change-type weight + tag weights + size, capped at 100. Noise scores 0–5.
- Front matter is found by line number (from the diff's hunk headers), so only Markdown front-matter lines
  count as metadata.
- Large edits (more than `TRACKER_LARGE_EDIT_LINES` changed lines) skip the character diff and, unless
  `TRACKER_LARGE_EDITS=off`, are summarised from the whole page cached in `cache/`.
- Label = the first of Breaking change, Retirement, GA, Preview, New (new page) that applies, else Doc update.

## Roadmap
- **v1 (this):** CAIP repositories (design Appendix A1), Azure Updates and product feeds, digests, discovery, web view.
- **v2:** Microsoft 365, Security and Business Applications sources (Appendix A2), Microsoft 365 roadmap, optional
  Message Center.
