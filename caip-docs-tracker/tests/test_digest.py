from __future__ import annotations

import json
import smtplib

import pytest
import requests
import yaml

from conftest import FakeResponse, run_main
from tracker import common, digest

NOW = common.utcnow().isoformat()
OLD = "2026-01-01T00:00:00+00:00"


def change(**kw) -> dict:
    base = {"scan_id": NOW, "repo_id": "azure-ai-docs", "week_start": "2026-10-05", "commit_date": "2026-10-06T10:00:00+00:00",
            "commit_sha": "abc", "commit_url": "https://gh/c/abc", "product": "Azure AI Search", "title": "Agentic retrieval",
            "file_path": "articles/search/a.md", "url": "https://learn/a", "change_summary": "Add GA article", "importance": "70",
            "noise": "false", "metadata_only": "false", "change_label": "GA", "tags": "ga", "key_lines": ""}
    return {**base, **kw}


def announcement(**kw) -> dict:
    base = {"feed_id": "azure-updates", "item_id": "u1", "published": "2026-10-06T09:00:00+00:00", "week_start": "2026-10-05",
            "fetched_at": NOW, "title": "Agentic retrieval is GA", "url": "https://upd/u1", "status": "Launched", "label": "GA",
            "products": "Azure AI Search", "categories": "Launched", "summary": "s"}
    return {**base, **kw}


def seed(project, rows: list[dict], anns: list[dict], summaries: dict | None = None, subs: list[dict] | None = None):
    common.write_csv(project / "data" / "azure-ai-docs" / "changes" / "2026-10-05.csv", common.CHANGE_FIELDS, rows)
    if anns:
        common.write_csv(project / "data" / "announcements" / "2026-10-05.csv", common.ANNOUNCEMENT_FIELDS, anns)
    if summaries:
        (project / "data" / "azure-ai-docs" / "summaries").mkdir(parents=True, exist_ok=True)
        week_file = {product: dict(by_week["2026-10-05"], row_count=1, provider="rule-based", generated_at=NOW)
                     for product, by_week in summaries.items()}
        (project / "data" / "azure-ai-docs" / "summaries" / "2026-10-05.json").write_text(json.dumps(week_file))
    if subs is not None:
        (project / "config" / "subscriptions.yml").write_text(yaml.safe_dump({"subscribers": subs}))


CFG = {"project": {"name": "Tracker", "significant_threshold": 30, "site_url": "https://site/"}}
SUMMARIES = {"Azure AI Search": {"2026-10-05": {"bullets": [
    {"headline": "Agentic retrieval GA", "detail": "Now GA.", "impact": "High", "label": "GA", "urls": ["https://learn/a"]},
    {"headline": "No label", "detail": "d", "impact": "Low"}]}}}


def test_build_filters_and_orders():
    rows = [change(), change(commit_sha="b", importance="10"), change(commit_sha="c", noise="true"),
            change(commit_sha="d", scan_id=OLD), change(commit_sha="e", product="API Management", change_label="Retirement"),
            change(commit_sha="f", change_label="", metadata_only="true"), change(commit_sha="g", change_label="", importance="")]
    anns = [announcement(), announcement(item_id="u2", fetched_at=OLD), announcement(item_id="u3", products="", label="Retirement")]
    ctx = digest.build({"id": "s"}, CFG, rows, anns, SUMMARIES, "2026-10-01")
    assert list(ctx["changes"]) == ["API Management", "Azure AI Search"]
    assert [r["commit_sha"] for r in ctx["changes"]["Azure AI Search"]] == ["abc"]
    # Retirement sorts before GA; an announcement naming no product goes to subscribers of every product.
    assert [a["item_id"] for a in ctx["announcements"]] == ["u3", "u1"]
    assert [h["headline"] for h in ctx["highlights"]] == ["Agentic retrieval GA", "No label"]
    assert ctx["name"] == "s" and ctx["more"] == 0 and ctx["site_url"] == "https://site/"

    mine = digest.build({"id": "s", "name": "N", "products": ["API Management"], "labels": ["Retirement"], "max_items": 1,
                         "include_announcements": False, "min_importance": 0}, CFG, rows, anns, {}, "2026-10-01")
    assert list(mine["changes"]) == ["API Management"] and mine["announcements"] == [] and mine["highlights"] == []
    capped = digest.build({"id": "s", "max_items": 1}, CFG, rows, anns, {}, "2026-10-01")
    assert capped["more"] == 1 and digest.is_empty(digest.build({"id": "s"}, CFG, [], [], {}, ""))


def test_wants():
    assert digest.wants({}, []) and digest.wants({"products": ["*"]}, ["x"])
    assert digest.wants({"products": ["a"]}, ["b", "a"]) and not digest.wants({"products": ["a"]}, [])


def ctx_full(**kw) -> dict:
    rows = [change(silent=True), change(commit_sha="x", url="", title="", product="API Management", change_label="")]
    ctx = digest.build({"id": "s", "name": "Team", "max_items": 1}, CFG, rows,
                       [announcement(), announcement(item_id="u9", products="")], SUMMARIES, "2026-10-01")
    ctx["announcements"] = [announcement(), announcement(item_id="u9", products="")]
    return {**ctx, **kw}


def test_render_markdown_and_html():
    ctx = ctx_full()
    md, page = digest.render_markdown(ctx), digest.render_html(ctx)
    for text in ("## Highlights", "## Announcements", "## Documentation changes", "(no matching announcement)",
                 "…and 1 more on the dashboard.", "[Open the dashboard](https://site/)", "(Azure AI Search)", digest.AI_NOTE):
        assert text in md
    assert "[link](https://learn/a)" in md and "[Doc update] No label" in md
    for text in ("<h2>Highlights</h2>", "<h2>Announcements</h2>", "<h3>Azure AI Search</h3>", "(no matching announcement)",
                 '<a href="https://site/">Open the dashboard</a>', "1 more on the dashboard"):
        assert text in page
    bare = ctx_full(highlights=[], announcements=[], more=0, site_url="")
    assert "Highlights" not in digest.render_markdown(bare) and "Open the dashboard" not in digest.render_html(bare)
    empty = ctx_full(highlights=[], announcements=[], changes={}, more=0, site_url="")
    assert "Documentation changes" not in digest.render_markdown(empty) + digest.render_html(empty)


def test_adaptive_card_and_trimming(monkeypatch):
    card = digest.adaptive_card(ctx_full())
    texts = [b["text"] for b in card["body"]]
    assert card["version"] == "1.4" and card["actions"][0]["url"] == "https://site/"
    assert "Highlights" in texts and "Announcements" in texts and digest.AI_NOTE in texts
    assert any(t.startswith("Documentation changes: ") for t in texts)
    plain = digest.adaptive_card(ctx_full(highlights=[], site_url=""))
    assert plain["actions"] == [] and digest.AI_NOTE not in [b["text"] for b in plain["body"]]
    monkeypatch.setattr(digest, "CARD_LIMIT", 1500)
    many = ctx_full(highlights=[], announcements=[announcement(item_id=str(i), title="x" * 50) for i in range(40)])
    trimmed = digest.adaptive_card(many)
    assert len(json.dumps(trimmed)) <= 1500 and len(trimmed["body"]) < 40
    huge = digest.adaptive_card(ctx_full(title="t" * 3000))
    assert len(huge["body"]) == 3  # nothing left to trim: title, subtitle and the AI note


def test_send_channels(http, monkeypatch):
    ctx = ctx_full()
    with pytest.raises(digest.NotConfigured):
        digest.send({"channel": "teams"}, ctx, "md", "<p>")
    monkeypatch.setenv("TEAMS_WEBHOOK_URL", "https://teams/hook")
    http.add("POST", "https://teams/hook", FakeResponse(202), FakeResponse(400, "bad card"))
    digest.send({"channel": "teams"}, ctx, "md", "<p>")
    assert http.calls[-1]["json"]["attachments"][0]["content"]["type"] == "AdaptiveCard"
    with pytest.raises(digest.DeliveryError, match="HTTP 400"):
        digest.send({}, ctx, "md", "<p>")

    monkeypatch.setenv("LOGIC_APP_URL", "https://logic/run")
    with pytest.raises(digest.NotConfigured, match="no recipients"):
        digest.send({"channel": "email"}, ctx, "md", "<p>")
    monkeypatch.setenv("TO_LIST", "a@x.com, b@x.com,")
    http.add("POST", "https://logic/run", FakeResponse(200))
    digest.send({"channel": "email", "to_env": "TO_LIST"}, ctx, "md", "<p>")
    assert http.calls[-1]["json"]["to"] == ["a@x.com", "b@x.com"] and http.calls[-1]["json"]["html"] == "<p>"
    with pytest.raises(digest.DeliveryError, match="unknown channel"):
        digest.send({"channel": "fax"}, ctx, "md", "<p>")


class FakeSMTP:
    sent: list = []

    def __init__(self, host, port, timeout):
        self.host, self.port, self.logged_in = host, port, None

    def __enter__(self):
        return self

    def __exit__(self, *a):
        return False

    def starttls(self):
        pass

    def login(self, user, password):
        self.logged_in = (user, password)

    def send_message(self, msg):
        FakeSMTP.sent.append((self.host, self.port, self.logged_in, msg))


def test_send_smtp(monkeypatch):
    monkeypatch.setattr(digest.smtplib, "SMTP", FakeSMTP)
    sub = {"channel": "email", "transport": "smtp", "to": ["a@x.com"]}
    monkeypatch.setenv("SMTP_FROM", "bot@x.com")
    with pytest.raises(digest.NotConfigured):
        digest.send(sub, ctx_full(), "md", "<p>")
    monkeypatch.setenv("SMTP_HOST", "smtp.x.com")
    digest.send(sub, ctx_full(), "md", "<p>")
    monkeypatch.setenv("SMTP_USERNAME", "u")
    monkeypatch.setenv("SMTP_PASSWORD", "p")
    monkeypatch.setenv("SMTP_PORT", "2525")
    digest.send(sub, ctx_full(), "md", "<p>")
    (h1, p1, l1, m1), (h2, p2, l2, _) = FakeSMTP.sent[-2:]
    assert (h1, p1, l1) == ("smtp.x.com", 587, None) and (p2, l2) == (2525, ("u", "p"))
    assert m1["To"] == "a@x.com" and m1.get_body(("html",)).get_content().strip() == "<p>"


def test_main_render_send_and_state(project, http, monkeypatch):
    subs = [{"id": "teams", "products": ["*"], "channel": "teams"},
            {"id": "mail", "products": ["Azure AI Search"], "channel": "email", "to": ["a@x.com"]},
            {"id": "apim", "products": ["API Management"], "channel": "teams"},
            {"id": "off", "enabled": False}]
    seed(project, [change(), change(commit_sha="old", scan_id=OLD)], [announcement()], SUMMARIES, subs)
    assert run_main(digest, monkeypatch) == 0  # render only
    files = sorted(p.name for p in (project / "data" / "digests").rglob("*.*"))
    assert files == ["mail.html", "mail.md", "teams.html", "teams.md"]
    md = next((project / "data" / "digests").rglob("teams.md")).read_text()
    assert "announced" not in md and "Agentic retrieval GA" in md and "old" not in md.split("diff")[0]
    assert common.load_json(project / "state" / "digests.json") == {}

    # --send: Teams not configured (skipped, not a failure); email delivery fails -> exit 1, state not advanced.
    monkeypatch.setenv("LOGIC_APP_URL", "https://logic/run")
    http.add("POST", "https://logic/run", FakeResponse(400), requests.ConnectionError("down"), FakeResponse(200))
    assert run_main(digest, monkeypatch, "--send") == 1
    assert common.load_json(project / "state" / "digests.json") == {}
    assert run_main(digest, monkeypatch, "--send", "--only", "mail") == 1  # network error
    assert run_main(digest, monkeypatch, "--send", "--only", "mail") == 0
    assert set(common.load_json(project / "state" / "digests.json")) == {"mail"}
    assert run_main(digest, monkeypatch, "--send", "--only", "mail") == 0  # nothing new since the last digest
    assert run_main(digest, monkeypatch, "--only", "nope") == 2


def test_main_smtp_errors_are_failures(project, monkeypatch):
    class Broken(FakeSMTP):
        def starttls(self):
            raise smtplib.SMTPException("tls failed")
    monkeypatch.setattr(digest.smtplib, "SMTP", Broken)
    monkeypatch.setenv("SMTP_FROM", "bot@x.com")
    monkeypatch.setenv("SMTP_HOST", "smtp.x.com")
    seed(project, [change()], [], None, [{"id": "m", "channel": "email", "transport": "smtp", "to": ["a@x.com"]}])
    assert run_main(digest, monkeypatch, "--send") == 1


def test_highlights_take_the_highest_impact_bullets():
    merged = {"Microsoft Foundry": {"2026-10-05": {"bullets": [
        {"headline": f"low {i}", "detail": "", "impact": "Low"} for i in range(3)] + [
        {"headline": "retirement", "detail": "", "impact": "High"}, {"headline": "odd", "detail": ""}]}}}
    ctx = digest.build({"id": "s"}, CFG, [change(product="Microsoft Foundry")], [], merged, "2026-10-01")
    assert [h["headline"] for h in ctx["highlights"]] == ["retirement", "odd", "low 0"]
