"""Bundle the CSV data into JSON files the static dashboard reads (site/data/*)."""
from __future__ import annotations

import json
from collections import Counter

from tracker.common import DATA_DIR, SITE_DIR, load_config, read_csv, utcnow, weeks_between

ROW_KEYS = ["repo_id", "week_start", "commit_date", "commit_sha", "commit_url", "pr_number", "author",
            "commit_subject", "change_summary", "change_type", "file_path", "old_path", "product", "title",
            "description", "url", "ms_service", "ms_subservice", "ms_topic", "ms_date", "ms_author",
            "lines_added", "lines_removed", "metadata_only", "tags", "importance", "key_lines"]


def main() -> None:
    cfg = load_config()
    thr = cfg["project"].get("significant_threshold", 30)
    rows, summaries = [], {}
    for src in cfg["sources"]:
        base = DATA_DIR / src["id"]
        for p in sorted((base / "changes").glob("*.csv")):
            rows.extend({k: r.get(k, "") for k in ROW_KEYS} for r in read_csv(p))
        for p in sorted((base / "summaries").glob("*.json")):
            for product, s in json.loads(p.read_text(encoding="utf-8")).items():
                summaries.setdefault(product, {})[p.stem] = s
    for r in rows:
        r["importance"] = int(r["importance"] or 0)
        r["lines_added"] = int(r["lines_added"] or 0)
        r["lines_removed"] = int(r["lines_removed"] or 0)
    rows.sort(key=lambda r: r["commit_date"], reverse=True)
    weeks = weeks_between(cfg["project"]["start_date"], utcnow().date().isoformat())
    total = Counter(r["week_start"] for r in rows)
    sig = Counter(r["week_start"] for r in rows if r["importance"] >= thr)
    pages = {}
    for r in rows:
        pages.setdefault(r["week_start"], set()).add(r["file_path"])
    products = Counter(r["product"] for r in rows)
    scans = read_csv(DATA_DIR / "scans.csv")
    meta = {
        "name": cfg["project"]["name"], "start_date": cfg["project"]["start_date"],
        "significant_threshold": thr, "generated_at": utcnow().isoformat(),
        "last_scan": scans[-1] if scans else None,
        "sources": [{"id": s["id"], "repo": s["repo"]} for s in cfg["sources"]],
        "weeks": weeks,
        "series": {"total": [total.get(w, 0) for w in weeks], "significant": [sig.get(w, 0) for w in weeks],
                   "pages": [len(pages.get(w, ())) for w in weeks]},
        "products": [{"name": n, "count": c} for n, c in products.most_common()],
        "tags": sorted({t for r in rows for t in r["tags"].split(";") if t}),
    }
    out = SITE_DIR / "data"
    out.mkdir(parents=True, exist_ok=True)
    (out / "meta.json").write_text(json.dumps(meta, indent=1), encoding="utf-8")
    (out / "changes.json").write_text(json.dumps(rows, ensure_ascii=False, separators=(",", ":")), encoding="utf-8")
    (out / "summaries.json").write_text(json.dumps(summaries, ensure_ascii=False, indent=1), encoding="utf-8")
    print(f"site data: {len(rows)} rows, {len(products)} products, {len(weeks)} weeks")


if __name__ == "__main__":
    main()
