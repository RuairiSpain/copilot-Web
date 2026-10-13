"""Read the collected data back: change rows, weekly summaries and announcements."""
from __future__ import annotations

import json

from tracker.common import DATA_DIR, load_yaml, DISCOVERED_PATH, read_csv


def all_sources(cfg: dict) -> list[dict]:
    """Every configured or discovered source, switched on or not, so disabling one keeps its history."""
    sources = list(cfg.get("sources", []))
    known = {s["id"] for s in sources}
    return sources + [s for s in load_yaml(DISCOVERED_PATH).get("sources", []) if s["id"] not in known]


def load_changes(cfg: dict) -> list[dict]:
    rows = []
    for src in all_sources(cfg):
        for p in sorted((DATA_DIR / src["id"] / "changes").glob("*.csv")):
            rows.extend(read_csv(p))
    return rows


def load_summaries(cfg: dict) -> dict[str, dict[str, dict]]:
    """{product: {week: summary}}. A product fed by several repos gets their bullets merged."""
    summaries: dict[str, dict[str, dict]] = {}
    for src in all_sources(cfg):
        for p in sorted((DATA_DIR / src["id"] / "summaries").glob("*.json")):
            for product, s in json.loads(p.read_text(encoding="utf-8")).items():
                by_week = summaries.setdefault(product, {})
                if p.stem in by_week:
                    merged = dict(by_week[p.stem])
                    merged["row_count"] += s["row_count"]
                    merged["bullets"] = merged["bullets"] + s["bullets"]
                    merged["provider"] = ", ".join(dict.fromkeys([merged["provider"], s["provider"]]))
                    s = merged
                by_week[p.stem] = s
    return summaries


def load_announcements() -> list[dict]:
    return [r for p in sorted((DATA_DIR / "announcements").glob("*.csv")) for r in read_csv(p)]
