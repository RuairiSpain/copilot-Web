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
| product | From the `products` path rules |
| title, description, url | Page metadata and Learn URL |
| ms_service, ms_subservice, ms_topic, ms_date, ms_author | Learn front matter |
| lines_added, lines_removed | Diff size |
| metadata_only | true if only front matter changed |
| tags | Semicolon-separated categories |
| importance | 0–100 |
| key_lines | Up to two changed lines that triggered a tag (evidence) |

## `data/<repo>/commits/<scan_date>.csv`
scan_id, repo_id, commit_sha, parents, commit_date, author, subject, files_changed, tracked_files_changed

`files_changed` is the number of files the commit changed (blank for merge commits); `tracked_files_changed` is the number of change rows written for it.

## `data/scans.csv`
scan_id, repo_id, run_at, branch, from_sha, to_sha, since, commits, tracked_changes, significant_changes, status

## `state/state.json`
`{ "<repo_id>": { "repo", "branch", "last_sha", "last_sync", "last_scan_id" } }`

## `data/<repo>/summaries/<week_start>.json`
`{ "<product>": { "row_count", "generated_at", "provider", "bullets": [ { "headline", "detail", "impact", "urls" } ] } }`

`provider` is the configured model provider, `rule-based` when the model was unavailable (retried next run), or `none` when the week had only minor or metadata-only edits (empty `bullets`).
