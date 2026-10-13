from __future__ import annotations

from tracker.correlate import correlate


def row(product: str, date: str, importance: int = 50, noise: str = "false", **kw) -> dict:
    return {"product": product, "commit_date": date, "importance": importance, "noise": noise, "metadata_only": "false", **kw}


def ann(product: str, date: str, title: str = "A") -> dict:
    return {"products": product, "published": date, "title": title, "url": f"https://a/{title}", "label": "GA"}


def test_correlate():
    anns = [ann("Azure AI Search", "2026-10-03T00:00:00+00:00", "near"), ann("Azure AI Search", "2026-09-01T00:00:00+00:00", "far"),
            ann("Azure AI Search;API Management", "2026-10-10T00:00:00+00:00", "multi"), ann("", "2026-10-03T00:00:00+00:00", "none")]
    rows = [row("Azure AI Search", "2026-10-04T00:00:00Z"), row("API Management", "2026-10-30T00:00:00Z"),
            row("API Management", "2026-10-30T00:00:00Z", importance=10), row("Azure AI Search", "2026-10-04T00:00:00Z", noise="true"),
            row("Other", "2026-10-04T00:00:00Z", importance="")]
    correlate(rows, anns, threshold=30)
    assert [a["title"] for a in rows[0]["announcements"]] == ["near", "multi"] and not rows[0]["silent"]
    assert rows[1]["announcements"] == [] and rows[1]["silent"]          # 20 days away: outside the window
    assert not rows[2]["silent"]                                          # below the threshold
    assert rows[3]["announcements"] and not rows[3]["silent"]             # noise is never silent
    assert not rows[4]["silent"]
    assert [a["doc_changes"] for a in anns] == [1, 0, 1, 0]               # the noise row is not counted
    correlate(rows[:1], anns[:1], threshold=30, window_days=0)
    assert rows[0]["announcements"] == [] and rows[0]["silent"]
