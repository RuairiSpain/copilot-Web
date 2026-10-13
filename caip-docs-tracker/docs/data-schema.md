# Data schema

## `data/<repo>/changes/<week_start>.csv` (one row per changed page per commit)
| Column | Description |
|---|---|
| scan_id | UTC timestamp of the scan that wrote the row |
| repo_id | Source id from `config/sources.yml` |
| week_start | Monday of the commit week (YYYY-MM-DD) |
| commit_date, commit_sha, commit_url, pr_number, author | Commit details |
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

## `data/scans.csv`
scan_id, repo_id, run_at, branch, from_sha, to_sha, since, commits, tracked_changes, significant_changes, status

## `state/state.json`
`{ "<repo_id>": { "repo", "branch", "last_sha", "last_sync", "last_scan_id" } }`

## `data/<repo>/summaries/<week_start>.json`
`{ "<product>": { "row_count", "generated_at", "provider", "bullets": [ { "headline", "detail", "impact", "urls" } ] } }`
