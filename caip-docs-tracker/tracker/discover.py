"""Phase 2: keep the source list complete by discovering documentation repositories.

Two methods, as in the design document (section 5):
  1. List the public repositories of the MicrosoftDocs organisation. A repository is a candidate
     when its name matches `discovery.include` (and not `exclude`), it was pushed to recently, it
     is not archived, it is not configured yet, and it has docfx.json or
     .openpublishing.publish.config.json at its root.
  2. Read the `original_content_git_url` meta tag of Microsoft Learn landing pages
     (`discovery.learn_pages`), which names the repository each product is published from.
     Learn points at the private "-pr" repository; the public mirror has the same name without it.

It also flags configured repositories that are archived or have had no push for `stale_days`.

Writes data/discovery.json (the report) and config/discovered.yml (a source block per candidate,
switched on only when `discovery.auto_add` is true; edit or copy a block into sources.yml to tune it).

Usage: python -m tracker.discover
"""
from __future__ import annotations

import datetime as dt
import fnmatch
import re
import sys

import yaml

from tracker.common import (
    DATA_DIR, DISCOVERED_PATH, github_headers, http_request, humanize, load_config, load_yaml, parse_iso, save_json,
    utcnow,
)

API = "https://api.github.com"
DOC_CONFIGS = ["docfx.json", ".openpublishing.publish.config.json"]
META_RE = re.compile(r"<meta\s+[^>]*name=[\"']original_content_git_url[\"'][^>]*>", re.I)
CONTENT_RE = re.compile(r"content=[\"']([^\"']+)[\"']", re.I)
REPO_RE = re.compile(r"github\.com/([^/]+)/([^/]+)/")


def list_repos(org: str) -> list[dict]:
    repos, page = [], 1
    while True:
        resp = http_request("GET", f"{API}/orgs/{org}/repos", headers=github_headers(),
                            params={"type": "public", "per_page": 100, "page": page, "sort": "pushed"})
        resp.raise_for_status()
        batch = resp.json()
        repos += batch
        if len(batch) < 100:
            return repos
        page += 1


def doc_config(full_name: str) -> str:
    """The first docs build config found at the repository root, or ''."""
    for name in DOC_CONFIGS:
        resp = http_request("GET", f"{API}/repos/{full_name}/contents/{name}", headers=github_headers())
        if resp.status_code == 200:
            return name
        if resp.status_code != 404:
            resp.raise_for_status()
    return ""


def learn_repo(url: str) -> str:
    """owner/repo of the public mirror a Learn page is published from, or ''."""
    resp = http_request("GET", url, timeout=30)
    resp.raise_for_status()
    tag = META_RE.search(resp.text)
    content = CONTENT_RE.search(tag.group(0)) if tag else None
    m = REPO_RE.search(content.group(1)) if content else None
    if not m:
        return ""
    return f"{m.group(1)}/{re.sub(r'-pr$', '', m.group(2))}"


def matches(name: str, patterns: list[str]) -> bool:
    return any(fnmatch.fnmatch(name.lower(), p.lower()) for p in patterns)


def source_block(repo: dict, config: str, enabled: bool, reason: str) -> dict:
    name = repo["name"]
    return {
        "id": name, "repo": repo["full_name"], "branch": repo.get("default_branch", "main"), "enabled": enabled,
        "discovered": {"reason": reason, "doc_config": config, "pushed_at": repo.get("pushed_at", "")},
        "include": ["**/*.md", "**/*.yml"],
        "exclude": ["**/media/**", "**/includes/**/media/**", "**/breadcrumb/**"],
        "default_product": humanize(re.sub(r"-docs?$", "", name)),
    }


def discover(cfg: dict, now: dt.datetime) -> dict:
    d = cfg.get("discovery", {})
    org = d.get("org", "MicrosoftDocs")
    configured = {s["repo"].lower() for s in cfg.get("sources", [])}
    recent = now - dt.timedelta(days=d.get("pushed_within_days", 30))
    stale_before = now - dt.timedelta(days=d.get("stale_days", 30))
    repos = list_repos(org)
    by_name = {r["full_name"].lower(): r for r in repos}

    stale = []
    for s in cfg.get("sources", []):
        r = by_name.get(s["repo"].lower())
        if r is None:
            if s["repo"].lower().startswith(org.lower() + "/"):
                stale.append({"repo": s["repo"], "reason": "not found in the organisation (renamed, split or private?)"})
        elif r.get("archived"):
            stale.append({"repo": s["repo"], "reason": "archived"})
        elif r.get("pushed_at") and parse_iso(r["pushed_at"]) < stale_before:
            stale.append({"repo": s["repo"], "reason": f"no push since {r['pushed_at'][:10]}"})

    reasons: dict[str, str] = {}
    for r in repos:
        if (r["full_name"].lower() not in configured and not r.get("archived") and r.get("pushed_at")
                and parse_iso(r["pushed_at"]) >= recent and matches(r["name"], d.get("include", ["*"]))
                and not matches(r["name"], d.get("exclude", []))):
            reasons[r["full_name"].lower()] = "name pattern and recent push"

    learn = []
    for page in d.get("learn_pages", []):
        try:
            repo = learn_repo(page["url"])
        except OSError as exc:
            learn.append({**page, "repo": "", "error": str(exc)[:200]})
            continue
        learn.append({**page, "repo": repo, "configured": repo.lower() in configured})
        if repo and repo.lower() not in configured and repo.lower() in by_name and not by_name[repo.lower()].get("archived"):
            reasons[repo.lower()] = f"Learn page for {page['product']}"

    candidates = []
    for full in sorted(reasons):
        r = by_name[full]
        config = doc_config(r["full_name"])
        if config:
            candidates.append({"repo": r["full_name"], "reason": reasons[full], "doc_config": config,
                               "pushed_at": r.get("pushed_at", ""), "repo_data": r})
    return {"generated_at": now.isoformat(), "org": org, "repos_listed": len(repos), "candidates": candidates,
            "stale": stale, "learn": learn}


def main() -> None:
    cfg = load_config()
    try:
        report = discover(cfg, utcnow())
    except OSError as exc:
        print(f"discovery FAILED: {exc}")
        sys.exit(1)
    auto = cfg.get("discovery", {}).get("auto_add", False)
    previous = {s["id"]: s for s in load_yaml(DISCOVERED_PATH).get("sources", [])}
    blocks = []
    for c in report["candidates"]:
        block = source_block(c.pop("repo_data"), c["doc_config"], auto, c["reason"])
        # Keep any edits made to an earlier block for the same repository.
        blocks.append(previous.get(block["id"], block))
    # A block someone switched on stays, even if the repository no longer qualifies as a candidate.
    listed = {b["id"] for b in blocks}
    blocks += [b for b in previous.values() if b.get("enabled") and b["id"] not in listed]
    DISCOVERED_PATH.write_text(
        "# Written by tracker/discover.py. Sources here are scanned only when `enabled: true`.\n"
        "# To tune one, copy it into sources.yml (which takes precedence) and edit it there.\n"
        + yaml.safe_dump({"sources": blocks}, sort_keys=False, allow_unicode=True), encoding="utf-8")
    save_json(DATA_DIR / "discovery.json", report)
    print(f"discovery: {report['repos_listed']} repos listed, {len(report['candidates'])} candidates, "
          f"{len(report['stale'])} stale sources")
    for c in report["candidates"]:
        print(f"  candidate {c['repo']} ({c['reason']}, {c['doc_config']})")
    for s in report["stale"]:
        print(f"  stale {s['repo']}: {s['reason']}")


if __name__ == "__main__":
    main()
