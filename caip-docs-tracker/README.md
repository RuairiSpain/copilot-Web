# CAIP Docs Change Tracker (POC: Azure AI docs)

Every **Thursday evening** a GitHub Actions workflow scans Microsoft Learn source repositories for
documentation changes since the last run. It saves the changes as CSV files under `data/`, writes a
ranked AI summary for each product, and publishes a dashboard to GitHub Pages.

This is a sub-project of the `copilot-Web` repository. Its workflows live at the repository root in
`.github/workflows/caip-docs-tracker-*.yml`, because GitHub only runs workflows from there, and run
with `caip-docs-tracker/` as the working directory. The design is in the Microsoft Docs Change Tracker
design document (v0.2); this POC is its Phase 0.

The POC tracks one repository, `MicrosoftDocs/azure-ai-docs`, which covers Foundry, Azure OpenAI,
AI Search, Machine Learning, Foundry Tools and Agent Framework. To track more Cloud & AI Platforms
repositories, add them to `config/sources.yml`.

## Project structure

```
.github/                         # at the repository root
├── workflows/
│   ├── caip-docs-tracker-weekly-scan.yml  # Thursday 18:00 UTC: scan → summarise → build → commit → deploy Pages
│   └── caip-docs-tracker-ci.yml           # On PR/push touching this folder: ruff, JS syntax, pytest, smoke test
└── ISSUE_TEMPLATE/caip-docs-tracker-add-source.md

caip-docs-tracker/
├── config/sources.yml           # Repos, paths, URL rules, product mapping, start date, AI model
├── tracker/                     # Python pipeline
│   ├── common.py                #   shared helpers (config, CSV, weeks)
│   ├── scan.py                  #   git history since last sync → change rows
│   ├── summarise.py             #   ranked AI summary per product/week
│   ├── build_site.py            #   CSV → JSON for the dashboard
│   └── check_urls.py            #   samples Learn URLs
├── data/                        # Written by the workflow (committed)
│   ├── scans.csv
│   └── azure-ai-docs/{changes,commits,summaries}/
├── state/state.json             # Watermark: last commit sha per repo
├── site/                        # Static dashboard (GitHub Pages)
│   ├── index.html               #   overview + weekly line chart
│   ├── product.html             #   AI summary + searchable/filterable table
│   ├── assets/                  #   css + js
│   └── data/                    #   generated JSON
├── docs/                        # architecture.md, data-schema.md
├── tests/                       # fake docs repo, pytest suite, smoke test
├── Makefile
├── requirements.txt
├── requirements-dev.txt         # pytest + ruff
├── ruff.toml
└── README.md
```

## How it works

```
Thursday 18:00 UTC (cron)                                     GitHub Pages
  scan.py ──► data/<repo>/changes/<week>.csv   ─┐
          ──► data/<repo>/commits/<scan>.csv    ├─► build_site.py ─► site/data/*.json ─► index.html (overview)
          ──► data/scans.csv, state/state.json  │                                       product.html (filter page)
  summarise.py ─► data/<repo>/summaries/<week>.json ─┘
```

1. **Scan** (`tracker/scan.py`)
   - Shallow-clones each repository, keeping only the history since the last sync.
   - Reads every commit between the stored watermark (`state/state.json` → `last_sha`) and the current `HEAD`. On the first run it reads every commit since `project.start_date`.
   - Writes the raw git history to the commits CSV.
   - Writes one row per changed page per commit to the changes CSV, with the page's metadata. Re-runs never write duplicate rows.
   - Takes the PR number from the commit message or, for commits on a merged branch, from the `Merge pull request #N` commit that brought them in.
   - Saves the new `HEAD` as the watermark.
2. **Classify.** Each row gets:
   - **Change type:** Added, Modified, Renamed or Deleted.
   - **Category tags:** `breaking-change`, `retirement`, `ga`, `preview`, `security`, `pricing-quota`, `models`, `region`, `api-sdk`, `new-page`, `removed-page`, `moved-page`, `navigation`, `whats-new`, `code-sample`, `minor-edit`, `metadata-only`.
   - **Importance score (0–100).** Edits that only touch front matter, such as `ms.date` or author, score 0 and are tagged `metadata-only`.
3. **Summarise** (`tracker/summarise.py`)
   - For each product and week, the up-to-40 most important rows go to an LLM. It returns a bullet list ranked by customer impact, each bullet labelled High, Medium or Low.
   - Bullets can only link to URLs from the input.
   - A product's week is summarised again whenever new rows arrive for it.
   - If no model is available, or the model call fails after retrying on rate limits, a rule-based summary is used instead. A rule-based fallback is retried on the next run when a model provider is configured.
   - A product whose week has only minor or metadata-only edits gets an empty summary and no model call.
4. **Publish** (`tracker/build_site.py`). Bundles the CSVs into JSON for the static dashboard.

## Data layout

| Path | Contents |
|---|---|
| `state/state.json` | Per repository: `last_sha` (watermark), `last_sync`, `branch`, `last_scan_id` |
| `data/scans.csv` | One line per repository per run: from/to sha, number of commits, rows, significant rows, status |
| `data/<repo>/commits/<scan-date>.csv` | Git history since the last sync, including merges: sha, parents, date, author, subject, tracked files changed |
| `data/<repo>/changes/<week-start>.csv` | One row per changed page per commit (schema below), bucketed by the Monday of the commit week |
| `data/<repo>/summaries/<week-start>.json` | Ranked AI bullets for each product |

**Change row columns:**
`scan_id, repo_id, week_start, commit_date, commit_sha, commit_url, pr_number, author, commit_subject,
change_summary, change_type, file_path, old_path, product, title, description, url, ms_service,
ms_subservice, ms_topic, ms_date, ms_author, lines_added, lines_removed, metadata_only, tags,
importance, key_lines`

- `change_summary` is the commit message with PR numbers removed. When the message is generic, such as "Update x.md", it is generated from the diff instead.
- `key_lines` holds up to two changed lines that triggered a tag, as evidence.

## Dashboard

- **Overview (`index.html`):**
  - headline counts: total, significant, latest week, new pages, retirement/breaking, preview/GA
  - a weekly line chart from the project start date showing all changes, significant changes and pages touched
  - a chart of change categories
  - a card for each product
- **Product page (`product.html?product=<name>`):**
  - **What changed:** the AI bullet list for the selected week, sorted by customer impact.
  - **Filters:** search across title, URL, commit message, author and path, plus product, week, change type, topic type, minimum importance, category tags (one or more) and a "hide minor & metadata-only" switch.
  - **Table** (sortable, 50 rows per page, CSV export): date, page title with link to Learn, file path, change type, tags, commit summary with link to the commit and PR, author, topic, `ms.date`, lines +/−, importance.

## Set-up (about 10 minutes)

1. The workflows run from this repository (`copilot-Web`). GitHub Pages on a private repository needs a paid plan, and a repository has one Pages site, so the dashboard is published at the repository's Pages URL.
2. Go to **Settings → Actions → General → Workflow permissions** and select **Read and write permissions**. The weekly workflow commits `caip-docs-tracker/data`, `state` and `site/data` to `main`; if `main` is protected, allow `github-actions[bot]` to push to it.
3. Go to **Settings → Pages → Build and deployment → Source** and select **GitHub Actions**.
4. Set the AI summary model, choosing one of these:
   - **GitHub Models (default).** No secret is needed, because the workflow uses its own token with `models: read`. Your organisation must allow GitHub Models.
   - **Azure OpenAI.** Set `summary.provider: "azure-openai"` and add the repository secrets `AZURE_OPENAI_ENDPOINT`, `AZURE_OPENAI_API_KEY` and `AZURE_OPENAI_DEPLOYMENT`.
5. Go to **Actions → caip-docs-tracker weekly scan → Run workflow** to run the first scan straight away. It backfills from `start_date`. After that, the scan runs every Thursday at 18:00 UTC, which is 20:00 in Madrid in summer and 19:00 in winter.
6. If any source fails to scan, the other sources are still summarised and published, and the run is marked failed by its **report** job. Check `data/scans.csv` for the error.
7. Check the **Check a sample of page URLs** step in the first run's log. If many URLs fail to resolve, adjust `url_rules` in `config/sources.yml`.

## Configuration (`config/sources.yml`)

- `project.start_date` sets where the backfill and the chart start. `significant_threshold` sets the importance score that counts as significant.
- `sources[]` takes one entry per repository:
  - `include` / `exclude` path patterns
  - `url_rules`, which map a path to its Learn URL
  - `products`, which map a path prefix to a product name. Edit these to rename or split product pages.
- To add another Cloud & AI Platforms repository, such as `MicrosoftDocs/azure-docs` for API Management, copy the `azure-ai-docs` block and change `id`, `repo`, the paths and the products.

## Local test

```bash
cd caip-docs-tracker
pip install -r requirements-dev.txt
ruff check .
pytest -q                       # unit tests + scan/summarise/build_site against a fake docs repo
bash tests/smoke_test.sh        # builds a fake docs repo and runs scan → summarise → build_site
python -m http.server -d <printed folder>/site 8000
```

## Known limitations (POC)

- **Product mapping** is based on folder path. `ms.service` is stored on each row if you'd rather map by that.
- **Learn URLs** are derived from the path. Check them with the URL step and tune `url_rules`. Files under `includes/` are not pages, so they get no URL.
- **Weeks** are bucketed by commit date in UTC, Monday start. Commits on a branch keep their own dates, so a branch merged late lands in the weeks it was written in. The Thursday scan sees changes up to Thursday evening. Friday-to-Sunday changes are added to that week by the next scan, and the week's summary is refreshed.
- **Public mirrors** can lag Microsoft's private source repositories by hours or days.
- **Content licence:** documentation content is CC BY 4.0. The dashboard shows short evidence lines and links back to the source rather than copying pages.
