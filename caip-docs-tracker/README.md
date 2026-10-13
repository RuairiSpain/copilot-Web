# CAIP Docs Change Tracker (v1: Cloud & AI Platforms)

A weekly digest of what changed in Microsoft documentation and announcements for Cloud & AI Platforms
(CAIP) products, organised by product. It follows the Microsoft Docs Change Tracker design document
(v0.2) and covers its v1 scope, Phases 0 to 2:

| Phase | Scope in the design | Where it is here |
|---|---|---|
| 0 | Azure Updates RSS, azure-ai-docs, APIM from azure-docs; weekly digest for Foundry, AI Search and APIM | `feeds` and `sources` in `config/sources.yml`; the `phase0-ai-apim` subscriber |
| 1 | All Appendix A1 repositories and feeds; per-product subscriptions; tuned noise filter | 11 repositories on, 10 more listed but off; `config/subscriptions.yml`; per-product RSS; noise filter in `scan.classify` |
| 2 | Automatic discovery limited to CAIP; web view | `tracker/discover.py` and its weekly workflow; the dashboard on GitHub Pages |

v2 (Microsoft 365, Security and Business Applications) is out of scope; discovery excludes those repositories.

This is a sub-project of the `copilot-Web` repository. Its workflows live at the repository root in
`.github/workflows/caip-docs-tracker-*.yml`, because GitHub only runs workflows from there, and run with
`caip-docs-tracker/` as the working directory.

## How it works

The two layers from the design:

- **Layer 1, announcements** (what Microsoft says changed): RSS/Atom feeds and GitHub discussions.
- **Layer 2, documentation** (what actually changed): the git history of MicrosoftDocs repositories.

Thursday 18:00 UTC, `caip-docs-tracker weekly scan`:

| Step | Module | Output |
|---|---|---|
| 1. Announcements | `announcements.py` | `data/announcements/<week>.csv` |
| 2. Documentation changes | `scan.py` | `data/<repo>/changes/<week>.csv`, `data/<repo>/commits/<date>.csv`, `data/scans.csv`, watermark in `state/state.json` |
| 3. Summaries | `summarise.py` | `data/<repo>/summaries/<week>.json` |
| 4. Web view | `build_site.py` (uses `correlate.py`) | `site/data/*.json`, `site/feeds/*.xml` |
| 5. URL check | `check_urls.py` | log only |
| 6. Digests | `digest.py --send` | Teams posts or emails, archived in `data/digests/<date>/` |
| 7. Publish | workflow | commits data and deploys `site/` to GitHub Pages |

Monday 06:00 UTC, `caip-docs-tracker discovery`: `discover.py` writes `data/discovery.json` and
`config/discovered.yml`.

### Collector (`scan.py`)

- For each enabled source, the collector shallow-clones the repository without a working tree or large blobs.
- It reads every commit from the stored watermark (the last processed SHA) to `HEAD`. If the watermark is missing, it reads commits since the last sync. On the first run it reads commits since `project.start_date`.
- It writes one row per changed page per commit. Each row has the page's front matter, its Learn URL, its product and the PR number. The PR number comes from the commit message, or from the `Merge pull request #N` commit that brought the commit in.
- Products are mapped in this order:
  1. the source's `products` path prefixes
  2. the page's `ms.service` (via `ms_service_products`)
  3. the folder name under `folder_products.prefix`
  4. `default_product`
- If one source fails, the other sources still run, and the scan exits non-zero at the end.

### Noise filter and labels

Changes are tagged (`breaking-change`, `retirement`, `ga`, `preview`, `security`, `pricing-quota`, `models`, `region`,
`api-sdk`, `new-page`, `removed-page`, `moved-page`, `navigation`, `include`, `whats-new`, `code-sample`) and scored 0–100.

A change is **noise** when it touches only metadata (`ms.date`, `author`, and so on), whitespace or links, when it is typo-sized (20 characters or fewer, with no keyword), or when it is a redirection file.

The following are never noise:
- new, deleted and moved files
- what's-new pages
- table of contents changes
- edits that contain a keyword such as "preview", "generally available", "retire", "deprecat" or "breaking"

Noise rows are kept in the CSVs, but summaries and digests leave them out, and the dashboard hides them by default.

Every change and every announcement gets one of the design's labels: **Breaking change, Retirement, GA, Preview, New, Doc update**.

### Announcements (`announcements.py`)

- **Feed types:** `rss`, `atom` and `github-discussions` (GraphQL, which needs `GITHUB_TOKEN`).
- **Conditional requests:** RSS and Atom feeds are fetched with `ETag` and `Last-Modified`, stored in `state/feeds.json`. The stored values are updated only after the new items are written.
- **Products:** an item's categories and title are matched to canonical products through `product_aliases`. An item that names no product falls back to the feed's own `product`.
- **Labels:** Azure Updates status categories set the label (`Launched` → GA, `In preview` → Preview, `Retirements` → Retirement). For other feeds, the label is read from the title.

### Correlator (`correlate.py`)

- A doc change and an announcement are linked when they are for the same product and published no more than `correlation_window_days` apart (default 14, either side).
- A significant, non-noise change with no linked announcement is a **silent change**.

### Summaries (`summarise.py`)

- **What is summarised:** for each product and week, up to 40 of the most important non-noise rows go to the model. The model returns bullets ranked by customer impact, each with an impact level and a label. Bullets can only link to URLs that were in the input.
- **Providers:** GitHub Models (the default) or Azure OpenAI.
- **When the model is unavailable:** a rule-based list is used instead, and the summary is retried on the next run. Rate limits (429) and server errors (5xx) are retried with backoff.
- **Weeks with only noise:** no model call is made, and the summary is empty.

### Digests (`digest.py`)

- **What a subscriber gets:** what is new since their last digest. A new subscriber gets the last 7 days. The digest has three parts:
  - the AI highlights for their products
  - matching announcements
  - significant doc changes, with silent changes marked and each change linked to its Learn page and its diff
- **Delivery:**
  - Teams: an Adaptive Card posted to a Teams "Workflows" webhook.
  - Email: an HTTP-triggered Logic App, or SMTP.
- **Secrets:** they come from environment variables named in `config/subscriptions.yml`. If a subscriber's secret is not set, their digest is archived but not sent, and the run does not fail.

### Discovery (`discover.py`)

- **Organisation listing:** lists the MicrosoftDocs public repositories. A repository becomes a candidate when it:
  - matches `discovery.include` and not `discovery.exclude`
  - has been pushed to within `pushed_within_days`
  - is not archived and not already configured
  - has `docfx.json` or `.openpublishing.publish.config.json` at its root
- **Learn pages:** reads the `original_content_git_url` meta tag of each page in `discovery.learn_pages`. The tag names the private `-pr` repository; the public mirror has the same name without `-pr`.
- **Stale sources:** flags configured repositories that are archived, missing from the organisation, or have had no push in `stale_days`.
- **Output:** each candidate becomes a source block in `config/discovered.yml`. Blocks are switched off unless `auto_add: true`. A block someone switches on stays, even if the repository stops qualifying as a candidate.

## Web view

- **Overview (`index.html`):**
  - headline counts, including announcements and silent changes
  - a weekly chart of changes, significant changes, pages touched, silent changes and announcements
  - change categories
  - a card for each product
  - the latest announcements
  - per-product RSS links
- **Product page (`product.html?product=<name>`):**
  - the AI summary for a selected week
  - the product's announcements
  - a searchable and filterable table of changes. Filters: product, week, change type, label, topic type, minimum importance, categories, "hide noise" and "silent changes only". Each row shows a "silent" marker or links to the matching announcements. The table can be exported as CSV.
- **RSS:** `feeds/all.xml` and `feeds/<product>.xml` list significant changes and announcements, so anyone can subscribe to a product without being added to `subscriptions.yml`.
- **Power BI:** the CSVs under `data/` can be read directly as a Power BI source (for example from the repository's raw URLs).

## Set-up

1. **Workflow permissions.** Go to **Settings → Actions → General → Workflow permissions** and select **Read and write permissions**. The workflows commit `caip-docs-tracker/{data,state,site/data,site/feeds,config/discovered.yml}` to `main`. If `main` is protected, allow `github-actions[bot]` to push to it.
2. **GitHub Pages.** Go to **Settings → Pages → Source** and select **GitHub Actions**. A repository has one Pages site, so the dashboard is published at this repository's Pages URL. Set `project.site_url` to that URL.
3. **AI model.**
   - **GitHub Models** (the default) needs no secret, but your organisation must allow GitHub Models.
   - **Azure OpenAI:** set `summary.provider: azure-openai` and add the secrets `AZURE_OPENAI_ENDPOINT`, `AZURE_OPENAI_API_KEY` and `AZURE_OPENAI_DEPLOYMENT`.
4. **Digests.** Add the secrets your subscribers use:
   - Teams: `TEAMS_WEBHOOK_URL`
   - Email through a Logic App: `LOGIC_APP_URL`, plus `DIGEST_PHASE0_TO` (comma-separated addresses)
   - Email through SMTP: `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD` and `SMTP_FROM`

   The Logic App receives `{to, subject, html, markdown}`.
5. **First run.** Go to **Actions → caip-docs-tracker weekly scan → Run workflow**. It backfills from `start_date`. If a feed, source or digest fails, the rest is still published and the `report` job marks the run as failed.
6. **Check the first run:**
   - The **Check a sample of page URLs** step. Tune `url_rules` where URLs do not resolve.
   - The product names on the dashboard. Tune `products`, `folder_products` and `product_aliases`.

## Configuration

- **`config/sources.yml`:**
  - `project`: start date, significance threshold, correlation window and site URL
  - `summary`: model provider and limits
  - `product_aliases`, `ms_service_products`
  - `feeds`, `discovery`
  - `sources`: for each source, `include`/`exclude` patterns, `url_rules`, `products`, `folder_products`, `default_product`, and `enabled` (default true)
- **`config/subscriptions.yml`:** the digest subscribers; the format is documented at the top of the file.
- **`config/discovered.yml`:** written by discovery. To tune a source block, copy it into `sources.yml`, which takes precedence.

## Local use

```bash
cd caip-docs-tracker
pip install -r requirements-dev.txt
ruff check .
pytest -q                       # unit and end-to-end tests; fails below 100% line and branch coverage
bash tests/smoke_test.sh        # fake docs repo → scan → summarise → build_site → digest
python -m tracker.scan --only azure-ai-docs   # one real source (clones it)
python -m tracker.digest        # render digests without sending
```

## Deviations from the design

- **Collector:** the design reads commits through the GitHub REST API. This uses `git` (a shallow, blob-filtered clone), which uses no API quota and gives the diffs in one call. The GitHub API is used only for discovery and discussions.
- **Store:** the design names Azure Table Storage or Cosmos DB. Watermarks, change records and digest state are CSV and JSON files committed to the repository: nothing to run or pay for, and the history is auditable. Subscriptions are a YAML file.
- **Delivery:** email goes through a Logic App or SMTP; Microsoft Graph `sendMail` is not implemented.

## Not yet verified

This code has been tested against a fake repository and mocked HTTP responses. It has not been run against the live sources. In particular:

- Paths, URL rules and product names for repositories other than azure-ai-docs follow their known layouts but have not been checked.
- The Azure Updates category names in `product_aliases` and the status labels have not been checked against the live feed.
- The `foundry-discussions` feed (needs the backing repository's name) and the Architecture Center Atom feed (needs the URL confirmed) are switched off.
- The `original_content_git_url` meta tag name comes from the design; the design notes it must be verified during build.

## Known limitations

- Weeks are bucketed by commit date in UTC, Monday start. Commits on a branch keep their own dates, so a branch merged late lands in the weeks it was written in, and that week's summary is refreshed.
- Public mirrors can lag Microsoft's private source repositories by hours or days.
- Documentation content is CC BY 4.0. Digests and the dashboard show short evidence lines and link back to the source rather than copying pages.
