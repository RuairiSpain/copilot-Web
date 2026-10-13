"""Correlator: link documentation changes to announcements for the same product.

A change matches an announcement when they share a product and are no more than `window_days`
apart (14 by default, either side). A significant, non-noise change with no match is a
"silent change": the documentation changed without an announcement.
"""
from __future__ import annotations

from collections import defaultdict

from tracker.common import is_true, parse_iso

WINDOW_DAYS = 14
MAX_MATCHES = 3


def correlate(rows: list[dict], announcements: list[dict], threshold: int, window_days: int = WINDOW_DAYS) -> None:
    """Add to each row `announcements` (up to 3 matching items, closest first) and `silent`;
    add to each announcement `doc_changes` (number of matching non-noise rows). Mutates its inputs."""
    by_product: dict[str, list[tuple[float, int]]] = defaultdict(list)
    for i, a in enumerate(announcements):
        a["doc_changes"] = 0
        for p in filter(None, a["products"].split(";")):
            by_product[p].append((parse_iso(a["published"]).timestamp(), i))
    window = window_days * 86400
    for r in rows:
        when = parse_iso(r["commit_date"]).timestamp()
        near = sorted((abs(t - when), i) for t, i in by_product.get(r["product"], []) if abs(t - when) <= window)
        matched = [announcements[i] for _, i in near[:MAX_MATCHES]]
        noise = is_true(r.get("noise")) or is_true(r.get("metadata_only"))
        if not noise:
            for _, i in near:
                announcements[i]["doc_changes"] += 1
        r["announcements"] = [{"title": a["title"], "url": a["url"], "label": a["label"]} for a in matched]
        r["silent"] = not matched and not noise and int(r["importance"] or 0) >= threshold
