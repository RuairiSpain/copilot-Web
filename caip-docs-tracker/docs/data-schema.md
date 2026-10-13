# Data schema

## `data/<repo>/changes/<week_start>.csv` (one row per changed page per commit)
| Column | Description |
|---|---|
| scan_id | UTC timestamp of the scan that wrote the row |
| repo_id | Source id from `config/sources.yml` |
| week_start | Monday of the commit week in UTC (YYYY-MM-DD) |
| commit_date, commit_sha, commit_url, pr_number, author | Commit details. `pr_number` comes from the commit message or from the merge commit that brought the commit in |
| commit_subject | Original commit message (first line) |
| change_summary | Cleaned commit message, or a summary generated from the diff when the message is generic |
| change_type | Added / Modified / Renamed / Deleted |
| file_path, old_path | Path in the repo (old_path is set for renames) |
| product | `products` prefixes, then `ms_service_products`, then `folder_products`, then `default_product` |
| title, description, url | Page metadata and Learn URL |
| ms_service, ms_subservice, ms_topic, ms_date, ms_author | Learn front matter |
| lines_added, lines_removed | Diff size |
| metadata_only | true if only front matter changed |
| tags | Semicolon-separated categories |
| importance | 0–100 |
| key_lines | Up to two changed lines that triggered a tag (evidence) |
| noise | true for metadata-only, whitespace-only, link-only, typo-sized and redirection-file changes |
| change_label | Breaking change / Retirement / GA / Preview / New / Doc update |

Files written before `noise` and `change_label` existed are rewritten with the new columns the next time a row is
appended to them; the site build treats their metadata-only and minor edits as noise.

## `data/<repo>/commits/<scan_date>.csv`
scan_id, repo_id, commit_sha, parents, commit_date, author, subject, files_changed, tracked_files_changed

`files_changed` is the number of files the commit changed (blank for merge commits); `tracked_files_changed` is the number of change rows written for it.

## `data/scans.csv`
scan_id, repo_id, run_at, branch, from_sha, to_sha, since, commits, tracked_changes, significant_changes, status

## `state/state.json`
`{ "<repo_id>": { "repo", "branch", "last_sha", "last_sync", "last_scan_id" } }`

## `data/<repo>/summaries/<week_start>.json`
`{ "<product>": { "row_count", "generated_at", "provider", "bullets": [ { "headline", "detail", "impact", "label", "urls" } ] } }`

`provider` is the configured model provider, `rule-based` when the model was unavailable (retried next run), or `none` when the week had only minor or metadata-only edits (empty `bullets`).

## `data/announcements/<week_start>.csv` (one row per feed item)
| Column | Description |
|---|---|
| feed_id, item_id | Feed id from `config/sources.yml` and the item's guid / id / link |
| published, week_start | Publication time (UTC) and its Monday |
| fetched_at | When the item was first collected (digests use it to pick what is new) |
| title, url, summary | Item text (summary is HTML-stripped, up to 500 characters) |
| status | The feed's own status category, if any (Azure Updates: Launched, In preview, Retirements) |
| label | Breaking change / Retirement / GA / Preview / New |
| products, categories | Semicolon-separated canonical products (via `product_aliases`) and the raw categories |

## `state/feeds.json`
`{ "<feed_id>": { "etag", "last_modified", "last_fetch" } }`

## `state/digests.json`
`{ "<subscriber_id>": "<time of the last digest sent>" }`

## `data/digests/<date>/<subscriber>.{md,html}`
The rendered digest, whether or not it was sent.

## `data/discovery.json`
`{ "generated_at", "org", "repos_listed", "candidates": [ { "repo", "reason", "doc_config", "pushed_at" } ],
"stale": [ { "repo", "reason" } ], "learn": [ { "product", "url", "repo", "configured" | "error" } ] }`

## `site/data/` (built, not edited)
`meta.json`, `changes.json` (rows plus `announcements` and `silent` from the correlator), `summaries.json`,
`announcements.json` (plus `doc_changes`). `site/feeds/all.xml` and `site/feeds/<product-slug>.xml` are RSS 2.0.
