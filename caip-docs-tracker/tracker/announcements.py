"""Layer 1: collect announcements (what Microsoft says changed) from RSS/Atom feeds and GitHub discussions.

For each feed in config/sources.yml `feeds`:
  1. Fetch it with a conditional request (ETag / Last-Modified from state/feeds.json); 304 means no news.
  2. Parse the items, map their categories and titles to canonical products (`product_aliases`)
     and give each one a change label (GA, Preview, Retirement, ...).
  3. Append new items, published on or after project.start_date, to data/announcements/<week>.csv.

Usage:
  python -m tracker.announcements              # all enabled feeds
  python -m tracker.announcements --only ID    # one feed
"""
from __future__ import annotations

import argparse
import datetime as dt
import email.utils
import html
import os
import re
import sys
import xml.etree.ElementTree as ET

from tracker.common import (
    ANNOUNCEMENT_FIELDS, DATA_DIR, FEED_STATE_PATH, append_csv, github_headers, http_request, load_config,
    load_json, parse_iso, product_aliases, products_matching, read_csv, save_json, utcnow, week_start,
)

ATOM = "{http://www.w3.org/2005/Atom}"
# Azure Updates status categories, and words that signal a label in a title.
STATUS_LABELS = {
    "launched": "GA", "generally available": "GA", "general availability": "GA", "now available": "GA",
    "in preview": "Preview", "preview": "Preview", "public preview": "Preview", "private preview": "Preview",
    "retirements": "Retirement", "retirement": "Retirement", "retiring": "Retirement",
    "in development": "New",
}
TITLE_LABELS = [
    ("Breaking change", re.compile(r"breaking change|action required|no longer supported", re.I)),
    ("Retirement", re.compile(r"\bretir(e|es|ed|ing|ement)\b|deprecat|end of (support|life)", re.I)),
    ("GA", re.compile(r"generally available|general availability|(?-i:\bGA\b)", re.I)),
    ("Preview", re.compile(r"\bpreview\b", re.I)),
]
DISCUSSIONS_QUERY = """query($owner:String!,$name:String!,$n:Int!){repository(owner:$owner,name:$name){
discussions(first:$n,orderBy:{field:CREATED_AT,direction:DESC}){nodes{id title url createdAt bodyText category{name}}}}}"""


def strip_html(text: str, limit: int = 500) -> str:
    text = re.sub(r"<[^>]+>", " ", text or "")
    text = " ".join(html.unescape(text).split())
    return text if len(text) <= limit else text[: limit - 1].rstrip() + "…"


def parse_date(value: str) -> str:
    """RSS (RFC 822) or Atom (ISO 8601) date -> ISO 8601 UTC; blank if unparsable."""
    value = (value or "").strip()
    if not value:
        return ""
    try:
        d = parse_iso(value)
    except ValueError:
        try:
            d = email.utils.parsedate_to_datetime(value)
        except (TypeError, ValueError):
            return ""
        if d.tzinfo is None:
            d = d.replace(tzinfo=dt.timezone.utc)
    return d.astimezone(dt.timezone.utc).replace(microsecond=0).isoformat()


def _text(el: ET.Element | None) -> str:
    return (el.text or "").strip() if el is not None else ""


def parse_feed(xml_text: str) -> list[dict]:
    """Items of an RSS 2.0 or Atom feed as {item_id, title, url, published, categories, summary}."""
    root = ET.fromstring(xml_text)
    items = []
    if root.tag == f"{ATOM}feed":
        for e in root.iter(f"{ATOM}entry"):
            links = e.findall(f"{ATOM}link")
            link = next((ln.get("href", "") for ln in links if ln.get("rel", "alternate") == "alternate"), "")
            url = link or (links[0].get("href", "") if links else "")
            items.append({
                "item_id": _text(e.find(f"{ATOM}id")) or url,
                "title": strip_html(_text(e.find(f"{ATOM}title")), 300),
                "url": url,
                "published": parse_date(_text(e.find(f"{ATOM}published")) or _text(e.find(f"{ATOM}updated"))),
                "categories": [c.get("term", "") for c in e.findall(f"{ATOM}category") if c.get("term")],
                "summary": strip_html(_text(e.find(f"{ATOM}summary")) or _text(e.find(f"{ATOM}content"))),
            })
    else:
        for e in root.iter("item"):
            url = _text(e.find("link"))
            items.append({
                "item_id": _text(e.find("guid")) or url,
                "title": strip_html(_text(e.find("title")), 300),
                "url": url,
                "published": parse_date(_text(e.find("pubDate"))),
                "categories": [_text(c) for c in e.findall("category") if _text(c)],
                "summary": strip_html(_text(e.find("description"))),
            })
    return items


def label_for(categories: list[str], title: str) -> tuple[str, str]:
    """(status, label): the feed's own status category if it has one, else a label read from the title."""
    for c in categories:
        if c.strip().lower() in STATUS_LABELS:
            return c.strip(), STATUS_LABELS[c.strip().lower()]
    for label, rx in TITLE_LABELS:
        if rx.search(title):
            return "", label
    return "", "New"


def fetch_discussions(feed: dict) -> list[dict] | None:
    """Recent discussions of a GitHub repository (GraphQL needs a token). None if no token."""
    if not os.environ.get("GITHUB_TOKEN"):
        print(f"[{feed['id']}] skipped: GITHUB_TOKEN is not set")
        return None
    resp = http_request("POST", "https://api.github.com/graphql", headers=github_headers(), json={
        "query": DISCUSSIONS_QUERY, "variables": {"owner": feed["owner"], "name": feed["repo"], "n": feed.get("limit", 30)}})
    resp.raise_for_status()
    payload = resp.json()
    if payload.get("errors"):
        raise RuntimeError(f"GraphQL error: {payload['errors'][0].get('message', payload['errors'])}")
    nodes = payload["data"]["repository"]["discussions"]["nodes"]
    wanted = feed.get("category")
    return [{
        "item_id": n["id"], "title": strip_html(n["title"], 300), "url": n["url"], "published": parse_date(n["createdAt"]),
        "categories": [n["category"]["name"]] if n.get("category") else [], "summary": strip_html(n.get("bodyText", "")),
    } for n in nodes if not wanted or (n.get("category") or {}).get("name") == wanted]


def fetch_feed(feed: dict, state: dict) -> tuple[list[dict] | None, dict]:
    """(items, cache validators) of one feed; items is None when the server says nothing changed (HTTP 304).

    The validators are saved only after the items are written, so a failed run does not hide them
    behind a 304 next time.
    """
    if feed.get("kind") == "github-discussions":
        return fetch_discussions(feed), {}
    st = state.get(feed["id"], {})
    headers = {"Accept": "application/rss+xml, application/atom+xml, application/xml;q=0.9, */*;q=0.5"}
    if st.get("etag"):
        headers["If-None-Match"] = st["etag"]
    if st.get("last_modified"):
        headers["If-Modified-Since"] = st["last_modified"]
    resp = http_request("GET", feed["url"], headers=headers, timeout=60)
    if resp.status_code == 304:
        print(f"[{feed['id']}] not modified")
        return None, {}
    resp.raise_for_status()
    validators = {"etag": resp.headers.get("ETag"), "last_modified": resp.headers.get("Last-Modified")}
    return parse_feed(resp.text), {k: v for k, v in validators.items() if v}


def collect(feed: dict, cfg: dict, state: dict, fetched_at: str) -> int:
    items, validators = fetch_feed(feed, state)
    if items is None:
        return 0
    start = parse_iso(cfg["project"]["start_date"] + "T00:00:00+00:00")
    aliases = product_aliases(cfg)
    by_week: dict[str, list[dict]] = {}
    for it in items:
        if not it["published"] or parse_iso(it["published"]) < start or not it["item_id"]:
            continue
        products = products_matching(aliases, it["categories"], it["title"])
        if not products and feed.get("product"):
            products = [feed["product"]]
        status, label = label_for(it["categories"], it["title"])
        row = {**it, "feed_id": feed["id"], "week_start": week_start(it["published"]), "fetched_at": fetched_at,
               "status": status, "label": label, "products": ";".join(products), "categories": ";".join(it["categories"])}
        by_week.setdefault(row["week_start"], []).append(row)
    written = 0
    for wk, rows in sorted(by_week.items()):
        target = DATA_DIR / "announcements" / f"{wk}.csv"
        seen = {(r["feed_id"], r["item_id"]) for r in read_csv(target)}
        unique: dict[tuple[str, str], dict] = {}
        for r in rows:  # the first copy of an item repeated within one fetch wins
            if (r["feed_id"], r["item_id"]) not in seen:
                unique.setdefault((r["feed_id"], r["item_id"]), r)
        fresh = list(unique.values())
        append_csv(target, ANNOUNCEMENT_FIELDS, fresh)
        written += len(fresh)
    state[feed["id"]] = {**validators, "last_fetch": fetched_at}
    print(f"[{feed['id']}] {len(items)} items, {written} new announcements")
    return written


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--only", help="fetch a single feed id")
    args = ap.parse_args()
    cfg = load_config()
    feeds = [f for f in cfg.get("feeds", []) if f.get("enabled", True)]
    if args.only and args.only not in {f["id"] for f in feeds}:
        ap.error(f"--only {args.only}: unknown feed id")
    state, fetched_at, failed = load_json(FEED_STATE_PATH), utcnow().isoformat(), []
    for feed in feeds:
        if args.only and feed["id"] != args.only:
            continue
        try:
            collect(feed, cfg, state, fetched_at)
        except Exception as exc:  # keep other feeds going; report at the end
            print(f"[{feed['id']}] FAILED: {exc}")
            failed.append(feed["id"])
    save_json(FEED_STATE_PATH, state)
    if failed:
        sys.exit(1)


if __name__ == "__main__":
    main()
