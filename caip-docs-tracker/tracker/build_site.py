"""Bundle the collected data into the files the static dashboard reads.

Writes site/data/{meta,changes,summaries,announcements}.json and one RSS feed per product
(site/feeds/<product>.xml, plus all.xml), so readers can subscribe to the products they follow.
"""
from __future__ import annotations

import email.utils
import json
from collections import Counter
from urllib.parse import quote
from xml.sax.saxutils import escape

from tracker.common import SITE_DIR, DATA_DIR, is_true, load_config, parse_iso, read_csv, slug, utcnow, weeks_between
from tracker.correlate import correlate
from tracker.store import all_sources, load_announcements, load_changes, load_summaries

ROW_KEYS = ["repo_id", "week_start", "commit_date", "commit_sha", "commit_url", "pr_number", "author",
            "commit_subject", "change_summary", "change_type", "file_path", "old_path", "product", "title",
            "description", "url", "ms_service", "ms_subservice", "ms_topic", "ms_date", "ms_author",
            "lines_added", "lines_removed", "metadata_only", "tags", "importance", "key_lines", "noise", "change_label"]
FEED_ITEMS = 50


def rss(title: str, link: str, description: str, items: list[dict]) -> str:
    def item(i: dict) -> str:
        return (f"<item><title>{escape(i['title'])}</title><link>{escape(i['link'])}</link>"
                f"<guid isPermaLink=\"false\">{escape(i['guid'])}</guid>"
                f"<pubDate>{email.utils.format_datetime(parse_iso(i['date']))}</pubDate>"
                f"<category>{escape(i['label'])}</category><description>{escape(i['description'])}</description></item>")
    return ('<?xml version="1.0" encoding="utf-8"?>\n<rss version="2.0"><channel>'
            f"<title>{escape(title)}</title><link>{escape(link)}</link><description>{escape(description)}</description>"
            + "".join(item(i) for i in items) + "</channel></rss>\n")


def feed_items(rows: list[dict], announcements: list[dict], thr: int) -> list[dict]:
    """Significant, non-noise doc changes and all announcements, newest first."""
    items = [{
        "title": f"[{r['change_label'] or 'Doc update'}] {r['product']}: {r['title'] or r['file_path']}",
        "link": r["url"] or r["commit_url"], "guid": f"{r['commit_sha']}:{r['file_path']}", "date": r["commit_date"],
        "label": r["change_label"] or "Doc update", "products": [r["product"]],
        "description": r["change_summary"] + (f" Evidence: “{r['key_lines']}”" if r["key_lines"] else "")
                       + (" (no matching announcement)" if r.get("silent") else ""),
    } for r in rows if r["importance"] >= thr and r["noise"] != "true"]
    items += [{
        "title": f"[{a['label']}] Announcement: {a['title']}", "link": a["url"], "guid": f"{a['feed_id']}:{a['item_id']}",
        "date": a["published"], "label": a["label"], "products": [p for p in a["products"].split(";") if p],
        "description": a["summary"],
    } for a in announcements]
    return sorted(items, key=lambda i: parse_iso(i["date"]), reverse=True)


def main() -> None:
    cfg = load_config()
    thr = cfg["project"].get("significant_threshold", 30)
    rows = [{k: r.get(k, "") for k in ROW_KEYS} for r in load_changes(cfg)]
    summaries = load_summaries(cfg)
    announcements = load_announcements()
    for r in rows:
        r["importance"] = int(r["importance"] or 0)
        r["lines_added"] = int(r["lines_added"] or 0)
        r["lines_removed"] = int(r["lines_removed"] or 0)
        # Rows written before the noise filter existed: treat metadata-only and minor edits as noise.
        if not r["noise"]:
            r["noise"] = str(is_true(r["metadata_only"]) or "minor-edit" in r["tags"].split(";")).lower()
    correlate(rows, announcements, thr, cfg["project"].get("correlation_window_days", 14))
    rows.sort(key=lambda r: r["commit_date"], reverse=True)
    announcements.sort(key=lambda a: a["published"], reverse=True)
    weeks = weeks_between(cfg["project"]["start_date"], utcnow().date().isoformat())
    total = Counter(r["week_start"] for r in rows)
    sig = Counter(r["week_start"] for r in rows if r["importance"] >= thr)
    silent = Counter(r["week_start"] for r in rows if r["silent"])
    ann = Counter(a["week_start"] for a in announcements)
    pages: dict[str, set] = {}
    for r in rows:
        pages.setdefault(r["week_start"], set()).add(r["file_path"])
    products = Counter(r["product"] for r in rows)
    ann_products = Counter(p for a in announcements for p in a["products"].split(";") if p)
    scans = read_csv(DATA_DIR / "scans.csv")
    meta = {
        "name": cfg["project"]["name"], "start_date": cfg["project"]["start_date"],
        "significant_threshold": thr, "generated_at": utcnow().isoformat(),
        "last_scan": scans[-1] if scans else None,
        "sources": [{"id": s["id"], "repo": s["repo"]} for s in all_sources(cfg) if s.get("enabled", True)],
        "feeds": [{"id": f["id"], "url": f.get("url") or f"https://github.com/{f.get('owner')}/{f.get('repo')}/discussions"}
                  for f in cfg.get("feeds", []) if f.get("enabled", True)],
        "weeks": weeks,
        "series": {"total": [total.get(w, 0) for w in weeks], "significant": [sig.get(w, 0) for w in weeks],
                   "pages": [len(pages.get(w, ())) for w in weeks], "silent": [silent.get(w, 0) for w in weeks],
                   "announcements": [ann.get(w, 0) for w in weeks]},
        "products": [{"name": n, "count": products.get(n, 0), "announcements": ann_products.get(n, 0), "slug": slug(n)}
                     for n in sorted(set(products) | set(ann_products), key=lambda n: (-products.get(n, 0), n))],
        "tags": sorted({t for r in rows for t in r["tags"].split(";") if t}),
        "labels": sorted({r["change_label"] for r in rows if r["change_label"]} | {a["label"] for a in announcements}),
    }
    out = SITE_DIR / "data"
    out.mkdir(parents=True, exist_ok=True)
    (out / "meta.json").write_text(json.dumps(meta, indent=1), encoding="utf-8")
    (out / "changes.json").write_text(json.dumps(rows, ensure_ascii=False, separators=(",", ":")), encoding="utf-8")
    (out / "summaries.json").write_text(json.dumps(summaries, ensure_ascii=False, indent=1), encoding="utf-8")
    (out / "announcements.json").write_text(json.dumps(announcements, ensure_ascii=False, separators=(",", ":")),
                                            encoding="utf-8")

    feeds = SITE_DIR / "feeds"
    feeds.mkdir(parents=True, exist_ok=True)
    items = feed_items(rows, announcements, thr)
    name, site_url = cfg["project"]["name"], cfg["project"].get("site_url", "")
    (feeds / "all.xml").write_text(rss(name, site_url, "Significant documentation changes and announcements",
                                       items[:FEED_ITEMS]), encoding="utf-8")
    for p in meta["products"]:
        mine = [i for i in items if p["name"] in i["products"]][:FEED_ITEMS]
        link = f"{site_url}product.html?product={quote(p['name'])}" if site_url else ""
        (feeds / f"{p['slug']}.xml").write_text(rss(f"{name}: {p['name']}", link,
                                                    f"Changes to {p['name']}", mine), encoding="utf-8")
    print(f"site data: {len(rows)} rows, {len(announcements)} announcements, {len(meta['products'])} products, "
          f"{len(weeks)} weeks, {sum(silent.values())} silent changes")


if __name__ == "__main__":
    main()
