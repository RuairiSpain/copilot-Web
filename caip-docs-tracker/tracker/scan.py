"""Weekly scan: collect documentation changes since the last stored commit.

For each source repo in config/sources.yml:
  1. Shallow-clone the branch (only history needed since the last sync).
  2. Read every commit since state[repo].last_sha (or since project.start_date on first run).
  3. Write the git history (commits CSV) and one row per changed page (changes CSV)
     with page metadata (title, URL, ms.* fields), line counts, tags and an importance score.
  4. Store the new HEAD sha as the watermark in state/state.json.

Usage:
  python -m tracker.scan                      # normal run (clones from GitHub)
  python -m tracker.scan --repo-path ID=PATH  # use an existing local clone (testing)
"""
from __future__ import annotations

import argparse
import datetime as dt
import re
import subprocess
import sys
import tempfile
from pathlib import Path

import yaml

from tracker.common import (
    CHANGE_FIELDS, COMMIT_FIELDS, DATA_DIR, SCAN_FIELDS, append_csv, first_prefix,
    load_config, load_state, parse_iso, path_matches, read_csv, save_state, utcnow, week_start,
)

# ---------------------------------------------------------------- classification rules
META_KEYS = re.compile(
    r"^\s*(title|titleSuffix|description|ms\.[\w-]+|author|manager|reviewer|services|keywords|"
    r"zone_pivot_groups|ROBOTS|ai-usage|update-frequency|recommendations|monikerRange|uid)\s*:.*$",
    re.I,
)
TAG_RULES: list[tuple[str, re.Pattern, int]] = [
    ("breaking-change", re.compile(r"breaking change|no longer supported|will be removed|must (migrate|update|upgrade)|action required|is not backward", re.I), 40),
    ("retirement", re.compile(r"\bretire(d|ment|s)?\b|deprecat|end of (support|life)|\bsunset", re.I), 35),
    ("ga", re.compile(r"generally available|general availability|(?-i:\bGA\b)", re.I), 25),
    ("preview", re.compile(r"\b(public |private )?preview\b", re.I), 20),
    ("security", re.compile(r"\bsecurity\b|vulnerab|\bCVE-|managed identity|\bRBAC\b|network isolation|private endpoint|encryption", re.I), 15),
    ("pricing-quota", re.compile(r"\bpric(e|ing)\b|\bbilling\b|\bquota|\brate limit|\blimits?\b|\bTPM\b|\bPTU", re.I), 15),
    ("models", re.compile(r"\bgpt-[\w.]+|\bo\d(-mini|-pro)?\b|\bphi-\d|\bllama|\bmistral|\bdeepseek|\bsora\b|\bwhisper\b|model (version|retirement|deprecation|availability)", re.I), 15),
    ("region", re.compile(r"\bregions?\b|now available in", re.I), 10),
    ("api-sdk", re.compile(r"api-version|\bREST API\b|\bSDK\b|\b\d{4}-\d{2}-\d{2}(-preview)?\b", re.I), 10),
]
WEIGHT_BY_TYPE = {"Added": 30, "Deleted": 35, "Renamed": 10, "Modified": 8}
GENERIC_SUBJECT = re.compile(
    r"^(learn editor:\s*)?(update|updated|edit|edits|modify|minor|fix|fixes|freshness|refresh|merge)\b.{0,80}$", re.I
)
PR_RE = re.compile(r"(?:\(#|pull request #)(\d+)")
CLONE_MARGIN_DAYS = 45


def run(cmd: list[str], cwd: Path | None = None, check: bool = True, input_bytes: bytes | None = None) -> str:
    res = subprocess.run(cmd, cwd=cwd, input=input_bytes, capture_output=True, check=False)
    if check and res.returncode != 0:
        raise RuntimeError(f"{' '.join(cmd)} failed: {res.stderr.decode(errors='replace')[:2000]}")
    return res.stdout.decode("utf-8", errors="replace")


def git(repo: Path, *args: str, check: bool = True) -> str:
    return run(["git", "-c", "core.quotePath=false", *args], cwd=repo, check=check)


# ---------------------------------------------------------------- repo access
def clone(repo: str, branch: str, since: dt.datetime, dest: Path) -> Path:
    # Commits on a branch keep their own dates, so a branch merged after a long review is older than
    # the merge. A wide margin keeps those commits in the shallow clone so the range log can see them.
    shallow = (since - dt.timedelta(days=CLONE_MARGIN_DAYS)).strftime("%Y-%m-%d")
    url = f"https://github.com/{repo}.git"
    run(["git", "clone", "--quiet", "--single-branch", "--branch", branch, "--no-tags",
         f"--shallow-since={shallow}", url, str(dest)])
    return dest


def has_commit(repo: Path, sha: str) -> bool:
    return bool(sha) and subprocess.run(
        ["git", "cat-file", "-e", f"{sha}^{{commit}}"], cwd=repo, capture_output=True).returncode == 0


def cat_files(repo: Path, specs: list[str]) -> dict[str, str | None]:
    """Read many 'rev:path' blobs in one git call."""
    out: dict[str, str | None] = {}
    if not specs:
        return out
    proc = subprocess.run(["git", "cat-file", "--batch"], cwd=repo,
                          input=("\n".join(specs) + "\n").encode(), capture_output=True)
    data, pos = proc.stdout, 0
    for spec in specs:
        nl = data.index(b"\n", pos)
        header = data[pos:nl].decode(errors="replace")
        pos = nl + 1
        if header.endswith("missing") or " ambiguous" in header:
            out[spec] = None
            continue
        size = int(header.split()[-1])
        out[spec] = data[pos:pos + size].decode("utf-8", errors="replace")
        pos += size + 1
    return out


def pr_by_commit(repo: Path, since_arg: list[str], rng: list[str]) -> dict[str, str]:
    """Map each commit to the pull request that merged it.

    Branch commits rarely mention their PR; the number is on the "Merge pull request #N" commit.
    Merges are read oldest first so a commit gets the PR that first brought it in, not a later
    branch-sync merge that also contains it.
    """
    out: dict[str, str] = {}
    raw = git(repo, "log", "--merges", "--reverse", "--format=%H%x1f%P%x1f%s", *since_arg, *rng)
    for line in raw.splitlines():
        _, parents, subject = (line.split("\x1f") + ["", ""])[:3]
        m, ps = PR_RE.search(subject), parents.split()
        if not m or len(ps) < 2:
            continue
        for sha in git(repo, "rev-list", f"{ps[0]}..{ps[1]}", check=False).split():
            out.setdefault(sha, m.group(1))
    return out


# ---------------------------------------------------------------- page metadata
def parse_page(path: str, text: str | None) -> dict:
    meta = {"title": "", "description": "", "ms_service": "", "ms_subservice": "",
            "ms_topic": "", "ms_date": "", "ms_author": ""}
    if not text:
        return meta
    fm: dict = {}
    try:
        if path.endswith(".md") and text.startswith("---"):
            end = text.find("\n---", 3)
            if end > 0:
                fm = yaml.safe_load(text[3:end]) or {}
        elif path.endswith(".yml"):
            doc = yaml.safe_load(text) or {}
            if isinstance(doc, dict):
                fm = dict(doc.get("metadata") or {})
                fm.setdefault("title", doc.get("title") or (doc.get("metadata") or {}).get("title"))
                fm.setdefault("description", doc.get("summary") or doc.get("description"))
    except yaml.YAMLError:
        for m in re.finditer(r"^([\w.]+):\s*(.+)$", text[:3000], re.M):
            fm.setdefault(m.group(1), m.group(2).strip().strip("'\""))
    if not isinstance(fm, dict):
        fm = {}
    meta["title"] = str(fm.get("title") or "")
    if not meta["title"] and path.endswith(".md"):
        h1 = re.search(r"^#\s+(.+)$", text, re.M)
        meta["title"] = h1.group(1).strip() if h1 else ""
    meta["description"] = str(fm.get("description") or "")[:300]
    meta["ms_service"] = str(fm.get("ms.service") or "")
    meta["ms_subservice"] = str(fm.get("ms.subservice") or "")
    meta["ms_topic"] = str(fm.get("ms.topic") or "")
    meta["ms_date"] = str(fm.get("ms.date") or "")
    meta["ms_author"] = str(fm.get("ms.author") or fm.get("author") or "")
    return meta


def learn_url(path: str, src: dict) -> str:
    if path.endswith(("toc.yml", "TOC.yml", ".json")) or "/includes/" in f"/{path}":
        return ""
    base, rule = first_prefix(path, src.get("url_rules", []), "base")
    if not rule:
        return ""
    rel = path[len(rule["prefix"]):]
    rel = re.sub(r"\.(md|yml)$", "", rel)
    rel = re.sub(r"(^|/)index$", r"\1", rel)
    return base + rel


# ---------------------------------------------------------------- diff parsing
def parse_log(raw: str) -> list[dict]:
    """Parse `git log -p --unified=0` output produced with our \x1e/\x1f format."""
    commits = []
    for block in raw.split("\x1e")[1:]:
        header, _, body = block.partition("\n")
        sha, parents, date, author, subject = (header.split("\x1f") + [""] * 5)[:5]
        files, cur, in_hunk = [], None, False
        for line in body.split("\n"):
            if line.startswith("diff --git "):
                m = re.match(r"diff --git a/(.*) b/(.*)$", line)
                cur = {"old": m.group(1) if m else "", "path": m.group(2) if m else "",
                       "status": "Modified", "added": [], "removed": [], "binary": False}
                files.append(cur)
                in_hunk = False
            elif cur is None:
                continue
            elif line.startswith("@@"):
                in_hunk = True
            elif in_hunk:
                # Inside a hunk every +/- line is content, including "--- x" or "+++ y".
                if line.startswith("+"):
                    cur["added"].append(line[1:])
                elif line.startswith("-"):
                    cur["removed"].append(line[1:])
            elif line.startswith("new file mode"):
                cur["status"] = "Added"
            elif line.startswith("deleted file mode"):
                cur["status"] = "Deleted"
            elif line.startswith("rename from "):
                cur["status"] = "Renamed"
                cur["old"] = line[12:]
            elif line.startswith("rename to "):
                cur["path"] = line[10:]
            elif line.startswith("Binary files"):
                cur["binary"] = True
        commits.append({"sha": sha, "parents": parents, "date": date, "author": author,
                        "subject": subject, "files": files})
    return commits


def strip_front_matter_lines(lines: list[str]) -> list[str]:
    return [ln for ln in lines if ln.strip() and not META_KEYS.match(ln) and ln.strip() != "---"]


def classify(f: dict, path: str, subject: str = "") -> tuple[list[str], int, bool, str]:
    added_body = strip_front_matter_lines(f["added"])
    removed_body = strip_front_matter_lines(f["removed"])
    metadata_only = f["status"] == "Modified" and not added_body and not removed_body
    tags: list[str] = []
    score = WEIGHT_BY_TYPE.get(f["status"], 8)
    low = path.lower()
    if low.endswith("toc.yml"):
        tags.append("navigation")
        score = 5
    elif f["status"] == "Added":
        tags.append("new-page")
    elif f["status"] == "Deleted":
        tags.append("removed-page")
    elif f["status"] == "Renamed":
        tags.append("moved-page")
    if "whats-new" in low or "whatsnew" in low or "release-notes" in low:
        tags.append("whats-new")
        score += 20
    body = added_body if f["status"] != "Deleted" else removed_body
    text = "\n".join(body)
    hits: list[str] = []
    for name, rx, weight in TAG_RULES:
        found = [ln for ln in body if rx.search(ln)]
        if found:
            tags.append(name)
            score += weight
            hits.extend(found[:2])
    # The commit message can carry the signal too (e.g. "Retire X page", "GA of Y").
    for name, rx, weight in TAG_RULES[:4]:
        if name not in tags and subject and rx.search(subject):
            tags.append(name)
            score += weight // 2
    if re.search(r"^```", text, re.M):
        tags.append("code-sample")
    churn = len(f["added"]) + len(f["removed"])
    score += min(20, churn // 10)
    if metadata_only:
        tags = ["metadata-only"]
        score = 0
    elif churn <= 3 and f["status"] == "Modified" and not hits and "navigation" not in tags:
        tags.append("minor-edit")
        score = min(score, 5)
    key = []
    for line in dict.fromkeys(hits):
        clean = re.sub(r"[*_`>#|]|\[([^\]]*)\]\([^)]*\)", r"\1", line).strip()
        if clean:
            key.append(clean[:200])
        if len(key) == 2:
            break
    return tags, min(100, score), metadata_only, " | ".join(key)


def change_summary(subject: str, status: str, title: str, added: int, removed: int, tags: list[str]) -> str:
    clean = PR_RE.sub("", subject)
    clean = re.sub(r"^(Learn Editor:\s*)", "", clean).strip(" -:()")
    if clean and not GENERIC_SUBJECT.match(clean) and len(clean) > 15:
        return clean
    if "navigation" in tags:
        return f'Navigation (TOC) updated: {title} (+{added}/-{removed} lines)'
    if "metadata-only" in tags:
        return f'Metadata-only edit to "{title or "untitled"}" (e.g. ms.date, author)'
    verb = {"Added": "New page", "Deleted": "Removed page", "Renamed": "Moved page"}.get(status, "Edited")
    extra = [t for t in tags if t not in {"new-page", "removed-page", "moved-page", "minor-edit"}]
    s = f'{verb}: "{title or "untitled"}" (+{added}/-{removed} lines)'
    return s + (f"; mentions {', '.join(extra)}" if extra else "")


# ---------------------------------------------------------------- main scan
def scan_source(src: dict, cfg: dict, state: dict, scan_id: str, local: Path | None) -> dict:
    repo_id = src["id"]
    st = state.get(repo_id, {})
    start = parse_iso(cfg["project"]["start_date"] + "T00:00:00+00:00")
    last_sync = parse_iso(st["last_sync"]) if st.get("last_sync") else start
    with tempfile.TemporaryDirectory() as tmp:
        repo = local or clone(src["repo"], src.get("branch", "main"), min(last_sync, utcnow()), Path(tmp) / repo_id)
        head = git(repo, "rev-parse", "HEAD").strip()
        last_sha = st.get("last_sha", "")
        if last_sha == head:
            print(f"[{repo_id}] no new commits since {head[:8]}")
            return {"scan_id": scan_id, "repo_id": repo_id, "run_at": scan_id, "branch": src.get("branch", "main"),
                    "from_sha": last_sha, "to_sha": head, "since": "", "commits": 0, "tracked_changes": 0,
                    "significant_changes": 0, "status": "no-change"}
        if has_commit(repo, last_sha):
            rng, since_arg, since_txt = [f"{last_sha}..{head}"], [], ""
        else:
            since_txt = last_sync.isoformat()
            rng, since_arg = [head], [f"--since={since_txt}"]
            if last_sha:
                print(f"[{repo_id}] watermark {last_sha[:8]} not in clone; falling back to --since {since_txt}")
        fmt = "--format=%x1e%H%x1f%P%x1f%cI%x1f%an%x1f%s"
        # Full git history since last sync (includes merges), for the commits CSV.
        hist = git(repo, "log", "--shortstat", fmt, *since_arg, *rng)
        history = []
        for b in hist.split("\x1e")[1:]:
            head_line, _, rest = b.partition("\n")
            h = dict(zip(["sha", "parents", "date", "author", "subject"],
                         (head_line.split("\x1f") + [""] * 5)[:5], strict=True))
            stat = re.search(r"(\d+) files? changed", rest)  # merges print no stat
            h["files_changed"] = stat.group(1) if stat else ""
            history.append(h)
        prs = pr_by_commit(repo, since_arg, rng)
        # Diffs of non-merge commits, limited to tracked top-level folders.
        tops = sorted({p.split("/")[0] for p in src.get("include", [])})
        raw = git(repo, "log", "--no-merges", "-M", "-p", "--unified=0", "--no-color", fmt,
                  *since_arg, *rng, "--", *tops)
        commits = parse_log(raw)

        rows, specs = [], []
        for c in commits:
            for f in c["files"]:
                path = f["path"]
                if f["binary"] or not path_matches(path, src.get("include")) or path_matches(path, src.get("exclude")):
                    continue
                spec = f"{c['sha']}^:{f['old']}" if f["status"] == "Deleted" else f"{c['sha']}:{path}"
                specs.append(spec)
                rows.append((c, f, spec))
        blobs = cat_files(repo, sorted(set(specs)))

    out, tracked_per_commit = [], {}
    gh = f"https://github.com/{src['repo']}"
    for c, f, spec in rows:
        path = f["path"]
        meta = parse_page(path, blobs.get(spec))
        if not meta["title"] and path.lower().endswith("toc.yml"):
            meta["title"] = f"Table of contents ({path.rsplit('/', 2)[-2]})"
        tags, score, meta_only, key = classify(f, path, c["subject"])
        product, _ = first_prefix(path, src.get("products", []), "product", src.get("default_product", "Other"))
        pr = PR_RE.search(c["subject"])
        pr_number = pr.group(1) if pr else prs.get(c["sha"], "")
        tracked_per_commit[c["sha"]] = tracked_per_commit.get(c["sha"], 0) + 1
        out.append({
            "scan_id": scan_id, "repo_id": repo_id, "week_start": week_start(c["date"]),
            "commit_date": c["date"], "commit_sha": c["sha"], "commit_url": f"{gh}/commit/{c['sha']}",
            "pr_number": pr_number, "author": c["author"], "commit_subject": c["subject"],
            "change_summary": change_summary(c["subject"], f["status"], meta["title"], len(f["added"]),
                                             len(f["removed"]), tags),
            "change_type": f["status"], "file_path": path,
            "old_path": f["old"] if f["status"] == "Renamed" else "",
            "product": product, "url": learn_url(f["old"] if f["status"] == "Deleted" else path, src),
            **meta, "lines_added": len(f["added"]), "lines_removed": len(f["removed"]),
            "metadata_only": str(meta_only).lower(), "tags": ";".join(tags), "importance": score, "key_lines": key,
        })

    # Only keep changes on/after the project start date.
    out = [r for r in out if parse_iso(r["commit_date"]) >= start]
    base = DATA_DIR / repo_id
    by_week: dict[str, list[dict]] = {}
    for r in out:
        by_week.setdefault(r["week_start"], []).append(r)
    written = 0
    for wk, items in sorted(by_week.items()):
        target = base / "changes" / f"{wk}.csv"
        seen = {(r["commit_sha"], r["file_path"]) for r in read_csv(target)}
        fresh = [r for r in items if (r["commit_sha"], r["file_path"]) not in seen]
        append_csv(target, CHANGE_FIELDS, fresh)
        written += len(fresh)
    append_csv(base / "commits" / f"{scan_id[:10]}.csv", COMMIT_FIELDS, [
        {"scan_id": scan_id, "repo_id": repo_id, "commit_sha": h["sha"], "parents": h["parents"],
         "commit_date": h["date"], "author": h["author"], "subject": h["subject"],
         "files_changed": h["files_changed"], "tracked_files_changed": tracked_per_commit.get(h["sha"], 0)}
        for h in history])
    sig = sum(1 for r in out if int(r["importance"]) >= cfg["project"].get("significant_threshold", 30))
    state[repo_id] = {"repo": src["repo"], "branch": src.get("branch", "main"), "last_sha": head,
                      "last_sync": utcnow().isoformat(), "last_scan_id": scan_id}
    print(f"[{repo_id}] {len(history)} commits, {written} new change rows ({sig} significant), head {head[:8]}")
    return {"scan_id": scan_id, "repo_id": repo_id, "run_at": scan_id, "branch": src.get("branch", "main"),
            "from_sha": last_sha, "to_sha": head, "since": since_txt, "commits": len(history),
            "tracked_changes": written, "significant_changes": sig, "status": "ok"}


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo-path", action="append", default=[], help="ID=PATH to use a local clone")
    ap.add_argument("--only", help="scan a single source id")
    args = ap.parse_args()
    local = dict(p.split("=", 1) for p in args.repo_path)
    cfg, state = load_config(), load_state()
    scan_id = utcnow().isoformat()
    logs = []
    ids = [s["id"] for s in cfg["sources"]]
    if args.only and args.only not in ids:
        ap.error(f"--only {args.only}: unknown source id (known: {', '.join(ids)})")
    for src in cfg["sources"]:
        if args.only and src["id"] != args.only:
            continue
        try:
            logs.append(scan_source(src, cfg, state, scan_id, Path(local[src["id"]]) if src["id"] in local else None))
        except Exception as exc:  # keep other repos going; record the failure
            print(f"[{src['id']}] FAILED: {exc}")
            logs.append({"scan_id": scan_id, "repo_id": src["id"], "run_at": scan_id, "status": f"error: {exc}"[:300]})
    append_csv(DATA_DIR / "scans.csv", SCAN_FIELDS, logs)
    save_state(state)
    if any(str(log["status"]).startswith("error") for log in logs):
        sys.exit(1)


if __name__ == "__main__":
    main()
