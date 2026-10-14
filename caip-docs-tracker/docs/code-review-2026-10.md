# Code review: caip-docs-tracker (October 2026)

A review of all code in `caip-docs-tracker/` and its workflows at commit `a72c9f3`. It is meant as the work list for
whoever fixes it next. Each finding gives the location, what goes wrong, a concrete case, the fix, and the test to add.

Line numbers are from `a72c9f3` and will drift; the function names are the stable reference.

**Rules for the fix work**

- Keep `pytest` at 100% line and branch coverage (CI enforces it) and `ruff check .` clean.
- Add a test that fails before each fix and passes after it.
- Do not change the data formats in `data/` as part of the defect fixes (C, M and L items). The layout proposal
  in section 4 is a separate change with its own migration.

Contents:
1. Defects: critical (C1–C9), medium (M1–M15), low (L1–L7)
2. Speed: parallel work and less work
3. Code structure
4. Output folders and sync state
5. Recommended order

---

## 1. Defects

### Critical: the data is wrong at scale

#### C1. Commits at the shallow-clone boundary are diffed against an empty tree
- **Where:** `tracker/scan.py`, `clone()` and `scan_source()` (the `git log -p` call, about line 506).
- **Problem:** the clone uses `--shallow-since`. A commit whose parent is older than the cut-off becomes a grafted
  root, and `git log -p` (with `log.showRoot` on by default) diffs it against an empty tree. Every tracked file in
  the repository is then reported as `Added`.
- **Reproduced:** in a two-commit repository, a one-line edit to `a.md` in the commit after the cut-off was shown as
  three new files (`a.md`, `b.md`, `c.md`). With `git -c log.showRoot=false` the commit showed no diff at all, which
  also loses the real edit.
- **When it happens:** always on the fallback path (watermark missing), because `since_txt` equals the clone cut-off.
  On normal runs, when a branch whose base is older than the cut-off is merged.
- **Impact:** on azure-docs, one such commit writes thousands of "New page" rows at importance 30 (the significance
  threshold). They are flagged silent and flood digests and RSS.
- **Fix:** after cloning (and after any fetch), run `git fetch --deepen=1` so boundary commits have a parent. Also
  read `.git/shallow` and drop any commit listed there from the diff log.
- **Test:** build a repository, shallow-clone it with a date that cuts one commit's parent, run `scan_source`, and
  assert that only the file actually edited is reported.

#### C2. Keywords are matched against whole changed lines, not against what changed
- **Where:** `tracker/scan.py`, `classify()`, the `TAG_RULES` loop (about line 395).
- **Problem:** a rule fires if the keyword appears anywhere in an added line, even when the removed version of that
  line already had it. A Markdown paragraph is one line, so any edit to a paragraph that mentions "deprecated", "GA"
  or "breaking change" gets that tag.
- **Example:** a typo fix in a paragraph that already says "This feature is deprecated" is tagged `retirement` (+35),
  labelled Retirement, scores 43 (above 30), is never noise (because `keep` is true when there are hits), and is
  flagged silent.
- **Fix:** count matches of each rule in the added body and in the removed body; tag only when the added count is
  greater (the keyword is new). Deleted pages keep the current behaviour (match the removed body).
- **Test:** an edit whose removed and added lines both contain "deprecated" gets no `retirement` tag; an edit that
  adds "deprecated" does.

#### C3. `churn <= 3` classes any edit of three lines or fewer as minor
- **Where:** `tracker/scan.py`, `classify()`, the `minor-edit` branch (about line 422).
- **Problem:** a Markdown paragraph is a single line. Replacing a whole paragraph is 1 removed + 1 added line, so it
  is tagged `minor-edit`, marked noise, capped at score 5 and hidden everywhere. `changed_chars()` is never called.
- **Fix:** drop the `churn <= 3` shortcut and decide typo size only by `changed_chars(...) <= TYPO_CHARS`.
  `changed_chars` is already bounded (it returns early for long text or unequal lengths), so this is safe for speed.
- **Test:** a one-line paragraph replaced with different text (no keywords) is not noise; a one-character typo fix is.

#### C4. Doubled product names for `azure-*` folders
- **Where:** `tracker/common.py`, `resolve_product()` (about line 165).
- **Problem:** `folder_products` adds `name_prefix: "Azure "` to `humanize(folder)` without removing a leading
  `azure-` from the folder name.
- **Example:** `articles/azure-resource-manager/x.md` becomes "Azure Azure Resource Manager"; `azure-vmware` becomes
  "Azure Azure Vmware". These names never match `product_aliases` or announcement products, so every significant
  change to them is flagged silent.
- **Fix:** if the folder starts with `azure-`, strip it before humanising (and do not double the prefix). Add `vmware`
  and `signalr` style casing to `ACRONYMS` or the `names` map where needed.
- **Test:** `resolve_product("articles/azure-resource-manager/x.md", src)` returns "Azure Resource Manager".

#### C5. A manual workflow run sends digests
- **Where:** `.github/workflows/caip-docs-tracker-weekly-scan.yml`, step "Send weekly digests".
- **Problem:** the step runs `digest --send` on every run, including `workflow_dispatch` runs used to re-scan one
  source mid-week.
- **Impact:** every subscriber gets an extra post or email, and `state/digests.json` moves forward, so Thursday's
  digest is split across two partial sends.
- **Fix:** run the step only when `github.event_name == 'schedule'`, or add a boolean dispatch input `send_digests`
  (default false) and check it.
- **Test:** none possible in pytest; check the workflow with `actionlint` if available, and state the condition in the
  PR description.

#### C6. An announcement is stored again when its date changes
- **Where:** `tracker/announcements.py`, `collect()` (about line 178).
- **Problem:** de-duplication only reads the CSV of the item's current `week_start`. If a feed changes an item's
  publication date (Azure Updates does when status changes), the same `(feed_id, item_id)` is appended to another
  week's file.
- **Impact:** the item appears twice on the dashboard and in RSS, is counted twice per product, and gets a new
  `fetched_at`, so it is sent again in the digest.
- **Fix:** before writing, load the set of `(feed_id, item_id)` from all announcement files (via
  `store.load_announcements()`), and skip any item already seen.
- **Test:** collect an item, then collect the same item with a pubDate one week later; assert one stored row.

#### C7. Feed links are not checked before being rendered as links
- **Where:** `tracker/announcements.py` (`parse_feed`, `fetch_discussions`); rendered in `site/assets/product.js`
  (about line 135), `site/assets/overview.js` (about line 82), and `tracker/digest.py` (`render_html`,
  `adaptive_card`).
- **Problem:** URLs from external feeds are only HTML-escaped. A `javascript:` link becomes a clickable script on the
  dashboard and in the digest email.
- **Fix:** in `collect()`, keep only items whose URL starts with `http://` or `https://` (blank the URL otherwise).
  As defence in depth, add a `safeUrl()` helper in `common.js` and use it for every `href` built from data.
- **Test:** a feed item with `<link>javascript:alert(1)</link>` is stored with an empty URL.

#### C8. One malformed YAML page stops the whole repository
- **Where:** `tracker/scan.py`, `parse_page()` (about line 153).
- **Problem:** `dict(doc.get("metadata") or {})` raises `ValueError` or `TypeError` when `metadata` is a string or a
  list. Only `yaml.YAMLError` is caught, so the exception reaches `main()` and the whole source is logged as an
  error. The watermark does not move, so it fails again every week.
- **Fix:** use the `metadata` value only if it is a dict. Also wrap the per-row work in `scan_source()` (parse_page,
  classify) so one bad page is logged and skipped instead of failing the source.
- **Test:** `parse_page("index.yml", "metadata: TBD\ntitle: T\n")` returns title "T" without raising.

#### C9. `**/` patterns are handled wrongly in two places
- **Where:** `tracker/common.py`, `path_matches()` (about line 121); `tracker/scan.py`, `scan_source()`, the `tops`
  pathspec (about line 505).
- **Problem 1:** `path_matches` treats only a `/**/` in the middle of a pattern as optional. A leading `**/` still
  needs a `/`, so `**/*.md` and `**/changelog.md` never match files at the repository root.
  architecture-center's root `changelog.md`, and root pages and `toc.yml` of azure-stack-docs and discovered sources,
  are silently dropped.
- **Problem 2:** the git pathspec is the first segment of each include pattern. A pattern starting with `**` gives
  the pathspec `**`, which removes the path limit, so `git log -p` diffs every file in the repository (all of
  architecture-center, for example). The output is parsed and then thrown away.
- **Fix:** in `path_matches`, also try the pattern with a leading `**/` removed. In `scan_source`, pass each include
  pattern to git as `:(glob)<pattern>` instead of computing `tops`.
- **Test:** `path_matches("changelog.md", ["**/changelog.md"])` is true; the pathspec list for
  `["**/changelog.md", "docs/**/*.md"]` contains no bare `**`.

### Medium: wrong numbers, wasted work, edge cases

#### M1. Feeds are decoded with the wrong character set
- **Where:** `tracker/announcements.py`, `fetch_feed()` (about line 155).
- **Problem:** the feed is parsed from `resp.text`. For `text/xml` or `text/*` without a charset, requests decodes
  it as ISO-8859-1, even though the XML declares UTF-8. Titles come out garbled.
- **Fix:** pass `resp.content` (bytes) to `ET.fromstring` so the XML declaration sets the encoding.
- **Test:** a UTF-8 feed with a curly apostrophe served as `text/xml` keeps the apostrophe.

#### M2. `changes.json` holds every row ever and is loaded by both pages
- **Where:** `tracker/build_site.py` (writing `changes.json`); `site/assets/overview.js` and `product.js`.
- **Problem:** the file has all rows from all weeks, noise included, with all columns. With 11 repositories it will
  reach tens of MB within months. The overview page downloads it only to compute totals.
- **Fix:** put the overview totals in `meta.json`. Write changes as one file per week (`site/data/changes/<week>.json`)
  and have the product page load only the weeks it shows. Optionally leave noise rows out of the default files.

#### M3. The commits CSV is written again on re-runs
- **Where:** `tracker/scan.py`, `scan_source()`, the commits `append_csv` (about line 567).
- **Problem:** the full `history` is appended without checking what is already stored. The fallback path and re-runs
  after a partial failure write the same commits again.
- **Fix:** read the existing commit SHAs for that file (or for the repository) and append only new ones.

#### M4. `significant_changes` counts rows from earlier scans
- **Where:** `tracker/scan.py`, `scan_source()` (about line 572).
- **Problem:** it counts over `out`, which includes rows already written before. A fallback rescan logs
  "0 new change rows (350 significant)".
- **Fix:** count over the rows actually written in this run (the `fresh` lists).

#### M5. The dashboard shows only one source's scan status
- **Where:** `tracker/build_site.py`, `meta["last_scan"]` (about line 300).
- **Problem:** it is the last row of `scans.csv`, which is the status of whichever source was logged last. A failure
  in another source is hidden.
- **Fix:** take all rows with the latest `scan_id` and report "ok" only if all succeeded; otherwise list the failed
  sources.

#### M6. The URL check samples deleted pages and always exits 0
- **Where:** `tracker/check_urls.py`.
- **Problem:** rows with `change_type == "Deleted"` point at removed pages and always return 404, which hides real
  URL-rule mistakes. `main()`'s return value is ignored under `__main__`, so the step never fails.
- **Fix:** skip deleted rows; exit non-zero when more than a set share (for example 20%) of sampled URLs fail. The
  workflow step already has `continue-on-error`, so this only makes the failure visible.

#### M7. Discovery ignores `auto_add: true` for repositories already listed
- **Where:** `tracker/discover.py`, `main()` (about line 420).
- **Problem:** an existing block in `discovered.yml` is always preferred over the new one, so switching `auto_add` on
  leaves earlier candidates with `enabled: false`.
- **Fix:** when `auto_add` is true, set `enabled: true` on the kept block (keep any other edits).

#### M8. Discovery lists repositories sorted by push time
- **Where:** `tracker/discover.py`, `list_repos()` (about line 311).
- **Problem:** `sort=pushed` reorders results when a repository is pushed during the listing, so repositories can be
  skipped or repeated between pages.
- **Fix:** use `sort=full_name`.

#### M9. Pages that start with a byte-order mark lose their front matter
- **Where:** `tracker/scan.py`, `front_matter_end()` and `parse_page()` (about line 280).
- **Problem:** both require the text to start with `---`. A page starting with `﻿---` is treated as having no
  front matter, so metadata edits count as content and `ms.service` is empty.
- **Fix:** strip a leading `﻿` before checking (in `cat_files` output or in both functions).

#### M10. A root `toc.yml` stops the source
- **Where:** `tracker/scan.py`, `scan_source()`, the TOC title fallback (about line 530).
- **Problem:** `path.rsplit('/', 2)[-2]` raises `IndexError` when the path has no `/`.
- **Fix:** use the parent folder name if there is one, else the repository id.

#### M11. Quiet repositories are cloned further back each week
- **Where:** `tracker/scan.py`, `scan_source()`, the "no new commits" return (about line 477).
- **Problem:** `last_sync` is not updated when nothing changed. The next clone uses `last_sync - 45 days`, so a repo
  with no commits for 3 months is cloned with 4.5 months of history every week, only to find nothing changed.
- **Fix:** base the clone date on the commit date of `last_sha` (see section 4), and check the branch head through
  the GitHub API before cloning (see section 2, item 2).

#### M12. The Teams card is trimmed without saying so
- **Where:** `tracker/digest.py`, `adaptive_card()` (about line 176).
- **Problem:** lines are dropped from the end until the card fits, but the card gives no hint that items are missing,
  and the subscriber's watermark still moves forward.
- **Fix:** when lines are dropped, add a final line "…and N more on the dashboard" with a link.

#### M13. Merging summaries from several repositories is wrong
- **Where:** `tracker/store.py`, `load_summaries()` (about line 317).
- **Problem:** the provider string is joined and then de-duplicated as a string, so three repos give
  "github-models, rule-based, github-models". The merged bullets are not re-ranked or capped at `max_bullets`.
- **Fix:** keep providers as a list (de-duplicate, then join once); sort merged bullets by impact and cap them.

#### M14. Two different paths can share a page-cache file
- **Where:** `tracker/common.py`, `page_cache_path()` (about line 194).
- **Problem:** `slug()` maps `/`, `-` and `.` to the same character, so `articles/a-b/c.md` and `articles/a/b-c.md`
  give the same filename. One large edit's text overwrites the other's.
- **Fix:** use a hash of the full path (for example the first 16 hex characters of SHA-256) in the filename.

#### M15. With no model configured, the log reports model failures
- **Where:** `tracker/summarise.py`, `main()` (about line 167).
- **Problem:** with provider `none` or no token, `call_llm` returns `None` immediately, which counts as a failure, so
  the log says "3 model calls failed in a row" although no model was configured.
- **Fix:** decide once at start whether a model is usable (provider and credentials present); if not, go straight to
  rule-based without counting failures.

### Low

- **L1.** The dashboard text hard-codes "14 days" (`overview.js` about line 43, `product.js` about line 198,
  `index.html`), while the window is `project.correlation_window_days`. Put the value in `meta.json` and use it.
- **L2.** `fmtDate` in `site/assets/common.js` parses a date-only string as UTC midnight but formats it in local
  time, so US viewers see `start_date` one day early. Format date-only values with `timeZone: "UTC"`.
- **L3.** The merge of configured and discovered sources exists twice (`store.all_sources()` and
  `common.enabled_sources()`). Move `all_sources` into `common` and define
  `enabled_sources = [s for s in all_sources(cfg) if s.get("enabled", True)]`.
- **L4.** `announcements.TITLE_LABELS` re-implements the label regexes that `scan.TAG_RULES` and `LABEL_BY_TAG`
  define, with differences ("sunset" and "must migrate" count for docs but not for announcements). Use one rule set
  (see section 3).
- **L5.** Unused code kept alive only by tests: `common.env_flag()`, `scan.run(input_bytes=...)`, `scan.git(check=...)`.
  Remove them and their tests.
- **L6.** `build_site.py` writes `site/data` and `site/feeds`, and the workflow commits them to `main` every week.
  They are build output; see section 4.
- **L7.** The fallback in `scan_source()` ignores whether `last_sha` is an ancestor of `HEAD`; a force-push is only
  detected when the commit is missing entirely. See section 4.

---

## 2. Speed: parallel work and less work

These are estimates. They have not been measured against the real repositories.

1. **Keep clones between runs.** Most of the time on azure-docs is the clone. Store each repository's clone in
   `actions/cache` (key: repo id; restore keys: repo id prefix), or use a self-hosted runner with a persistent disk,
   and run `git fetch` each week instead of cloning. Then `git fetch --deepen=1` for C1.
2. **Check for new commits before cloning.** Call `GET /repos/{owner}/{repo}/commits/{branch}` and compare the SHA
   with `state.last_sha`; if equal, record "no-change" and skip the clone. One API call per quiet repository.
3. **Scan sources in parallel.** Sources are independent and the work is in `git` subprocesses, so a
   `concurrent.futures.ThreadPoolExecutor` with 4–6 workers is enough. Each worker returns its scan-log row and new
   state; the main thread writes `scans.csv` and the state files. On GitHub Actions the larger option is a matrix
   job (one runner per source, each with its own disk and time limit) followed by a gather job that merges results.
   That needs one state file per source (section 4).
4. **Read git output as a stream.** `run()` collects all of `git log -p` into one string, then splits it. Use
   `subprocess.Popen(..., stdout=PIPE)` and parse line by line so memory stays flat and parsing overlaps git's work.
5. **Split large repositories.** For azure-docs, run one `git log -p` per group of second-level folders in parallel
   (each with its own pathspec), then merge. Git uses one core per log; runners have 4.
6. **Summarise in parallel within the rate limit.** Use a pool of about 4 workers for model calls, with a shared rate
   limiter (GitHub Models allows about 15 requests a minute on the low tier) and the failure cut-off under a lock.
7. **Discovery in one query.** One GraphQL query per 100 repositories can return `name`, `pushedAt`, `isArchived`,
   `defaultBranchRef` and `object(expression: "HEAD:docfx.json") { id }`. That replaces 8 list calls plus one
   contents call per candidate.
8. **Build the site from small files.** See M2.

---

## 3. Code structure

- **Split `tracker/scan.py` (about 600 lines)** into:
  - `git.py`: clone, fetch, deepen, streaming log, `cat-file`
  - `diff.py`: `parse_log`, `header_paths`, `unquote_path`
  - `pages.py`: `parse_page`, `front_matter_end`, `learn_url`
  - `classify.py`: tag rules, noise, labels, `changed_chars`; also used by `announcements.py` (fixes L4)
  - `scan.py`: running the steps in order
- **Move tag regexes and weights into `config/rules.yml`**, loaded once, so they can be tuned without code changes.
- **Typed records.** Replace dicts of strings with dataclasses (`Change`, `Announcement`, `Summary`). Today
  `"true"`/`"false"` strings and `int(r["importance"] or 0)` conversions are spread across five modules. Convert at
  the CSV/JSON boundary only.
- **Validate the config on load** (pydantic or JSON Schema), so a typo in `sources.yml` stops the run with a clear
  message.
- **One entry point:** `python -m tracker <scan|announcements|summarise|site|digest|discover>`.
- **Use `logging`** instead of `print`, with the source id on each line.

---

## 4. Output folders and sync state

### Problems with the current layout

- Weekly CSVs are keyed by commit date, so a late-merged branch appends to a past week's file.
- `data/scans.csv` grows without limit.
- Generated `site/data` and `site/feeds` are committed to `main` every week.
- CSV cannot hold the lists the tracker now produces (tags, matched announcements) without packing them into strings.
- All sources share `state/state.json`, so parallel or matrix jobs would conflict on it.

### Proposed layout

A dedicated `tracker-data` branch, so `main` stays free of weekly data commits and outside branch protection:

```
state/
  sources/<repo_id>.json        one file per source: no conflicts between parallel or matrix jobs
  feeds/<feed_id>.json
  digests/<subscriber>.json
runs/<yyyy>/<run_id>.json       what each run did: per source from/to SHA, counts, status, duration
raw/<repo_id>/<yyyy>/<run_id>.jsonl   rows as collected; add-only, never rewritten
weekly/<yyyy>-W<ww>/<product-slug>.json   one item per page per week, plus the summary
pages/<repo_id>.jsonl           latest metadata per page: title, URL, product, last change
announcements/<yyyy>/<week>.jsonl
digests/<yyyy>/<date>/<subscriber>.md
```

- **Row ID:** each row gets a stable id, `sha256(repo_id + commit_sha + path)`. De-duplication checks one index
  instead of per-week files (also fixes C6's announcement equivalent: `sha256(feed_id + item_id)`).
- **`weekly/` is the published output.** The digest, the dashboard and any future MCP server read it.
- **`site/` is built in CI and deployed, not committed.**
- **Formats:** JSONL for anything added to over time; Parquet only if Power BI needs it.
- **Migration:** a one-off script converts existing `data/<repo>/changes/*.csv`, `data/announcements/*.csv` and the
  `state/*.json` files to the new layout.

### Remembering the last sync

Each source's state file:

```json
{
  "repo": "MicrosoftDocs/azure-docs",
  "branch": "main",
  "last_sha": "…",
  "last_commit_date": "2026-10-09T17:02:11Z",
  "last_run_id": "2026-10-15T18:00Z",
  "last_status": "ok",
  "rows_written": 412,
  "schema_version": 2
}
```

- **Base the clone depth on `last_commit_date`** (the date of `last_sha`), not on when the scan last ran. This fixes
  M11 and makes the fallback window predictable.
- **Detect force-pushes.** Before reading `last_sha..HEAD`, run `git merge-base --is-ancestor <last_sha> HEAD`. If it
  fails, history was rewritten: fall back to the date window and rely on the row ID to avoid duplicates (fixes L7).
- **Write files safely.** Write to a temporary file and rename it, so a failed run cannot leave half a file.
- **Advance the watermark last.** Commit the rows and the run record first; update `last_sha` only after that commit
  is pushed. Today a failed push loses the week's state, but the next run repeats the work and duplicates are removed,
  so nothing is lost. This change keeps that guarantee once jobs run in parallel.
- **If the data later moves to Azure,** the same files map onto Blob Storage. Use ETag-conditional writes so two runs
  cannot overwrite each other. This is the design document's Table Storage option without changing the code above.

---

## 5. Recommended order

1. **Fix C1–C9**, then M1–M15 and L1–L5, in PR #28 or a follow-up PR. These are correctness fixes; each needs a test.
2. **Speed:** keep clones between runs, check heads before cloning, scan sources in parallel (section 2, items 1–3).
3. **The `tracker-data` branch, the new layout and page-week grouping** (section 4), as a separate PR with the
   migration script. Do the module split (section 3) at the same time.
