from __future__ import annotations

import json
import xml.etree.ElementTree as ET

from conftest import run_main, set_config
from tracker import build_site, check_urls, common, store

NOW = common.utcnow().isoformat()


def change(**kw) -> dict:
    base = {"scan_id": NOW, "repo_id": "azure-ai-docs", "week_start": "2026-10-05", "commit_date": "2026-10-06T10:00:00+00:00",
            "commit_sha": "abc", "commit_url": "https://gh/c/abc", "product": "Azure AI Search", "title": "Agentic retrieval",
            "file_path": "articles/search/a.md", "url": "https://learn/a", "change_summary": "Add GA article", "importance": "70",
            "noise": "false", "metadata_only": "false", "change_label": "GA", "tags": "ga", "key_lines": "now GA",
            "lines_added": "3", "lines_removed": "", "ms_topic": "concept-article"}
    return {**base, **kw}


def test_store_merges_summaries_across_repos(project):
    (project / "config" / "discovered.yml").write_text("sources:\n- {id: azure-docs-extra, repo: MicrosoftDocs/x, enabled: false}\n"
                                                       "- {id: azure-ai-docs, repo: dup}\n")
    assert [s["id"] for s in store.all_sources(common.load_config())][-1] == "azure-docs-extra"
    for repo_id, headline in (("azure-ai-docs", "a"), ("azure-docs", "b"), ("azure-docs-extra", "c")):
        d = project / "data" / repo_id / "summaries"
        d.mkdir(parents=True)
        (d / "2026-10-05.json").write_text(json.dumps({"Azure OpenAI": {
            "row_count": 2, "generated_at": NOW, "provider": "github-models" if headline != "c" else "rule-based",
            "bullets": [{"headline": headline, "detail": "", "impact": "Low", "urls": []}]}}))
    merged = store.load_summaries(common.load_config())["Azure OpenAI"]["2026-10-05"]
    assert merged["row_count"] == 6 and [b["headline"] for b in merged["bullets"]] == ["a", "b", "c"]
    assert merged["provider"] == "github-models, rule-based"


def test_build_site(project, monkeypatch):
    rows = [change(), change(commit_sha="m", noise="", tags="minor-edit", importance="2", change_label=""),
            change(commit_sha="n", noise="", metadata_only="true", importance="0"),
            change(commit_sha="s", commit_url="https://gh/c/s", product="API Management", url="", commit_date="2026-10-07T10:00:00+00:00", key_lines=""),
            change(commit_sha="o", noise="", importance="", lines_added="", change_label="")]
    common.write_csv(project / "data" / "azure-ai-docs" / "changes" / "2026-10-05.csv", common.CHANGE_FIELDS, rows)
    common.write_csv(project / "data" / "announcements" / "2026-10-05.csv", common.ANNOUNCEMENT_FIELDS, [
        {"feed_id": "azure-updates", "item_id": "u1", "published": "2026-10-05T09:00:00+00:00", "week_start": "2026-10-05",
         "fetched_at": NOW, "title": "Agentic retrieval GA", "url": "https://upd/u1", "status": "Launched", "label": "GA",
         "products": "Azure AI Search", "categories": "", "summary": "s"},
        {"feed_id": "foundry-devblog", "item_id": "b1", "published": "2026-10-08T09:00:00+00:00", "week_start": "2026-10-05",
         "fetched_at": NOW, "title": "Foundry news", "url": "https://blog/b1", "status": "", "label": "New",
         "products": "Microsoft Foundry", "categories": "", "summary": "b"}])
    common.append_csv(project / "data" / "scans.csv", common.SCAN_FIELDS, [{"scan_id": NOW, "status": "ok", "run_at": NOW}])
    assert run_main(build_site, monkeypatch) == 0
    out = project / "site" / "data"
    meta = json.loads((out / "meta.json").read_text())
    changes = {r["commit_sha"]: r for r in json.loads((out / "changes.json").read_text())}
    assert changes["abc"]["announcements"][0]["url"] == "https://upd/u1" and not changes["abc"]["silent"]
    assert changes["s"]["silent"] and changes["m"]["noise"] == "true" and changes["n"]["noise"] == "true"
    assert changes["o"]["noise"] == "false" and changes["o"]["importance"] == 0 and changes["o"]["lines_added"] == 0
    assert [p["name"] for p in meta["products"]] == ["Azure AI Search", "API Management", "Microsoft Foundry"]
    assert meta["products"][2] == {"name": "Microsoft Foundry", "count": 0, "announcements": 1, "slug": "microsoft-foundry"}
    assert meta["last_scan"]["status"] == "ok" and meta["series"]["silent"][-2:] != []
    assert {f["id"] for f in meta["feeds"]} == {"azure-updates", "foundry-devblog"}
    assert "GA" in meta["labels"] and "New" in meta["labels"]
    assert len(json.loads((out / "announcements.json").read_text())) == 2

    feeds = project / "site" / "feeds"
    all_items = ET.parse(feeds / "all.xml").getroot().findall("./channel/item")
    assert [i.findtext("guid") for i in all_items] == ["foundry-devblog:b1", "s:articles/search/a.md", "abc:articles/search/a.md",
                                                       "azure-updates:u1"]
    assert "(no matching announcement)" in all_items[1].findtext("description")
    assert all_items[1].findtext("link") == "https://gh/c/s"
    search = ET.parse(feeds / "azure-ai-search.xml").getroot()
    assert search.findtext("./channel/link") == "https://ruairispain.github.io/copilot-Web/product.html?product=Azure%20AI%20Search"
    assert len(search.findall("./channel/item")) == 2

    set_config(project, lambda c: c["project"].pop("site_url"))
    common.write_csv(project / "data" / "scans.csv", common.SCAN_FIELDS, [])
    (project / "data" / "scans.csv").unlink()
    assert run_main(build_site, monkeypatch) == 0
    assert json.loads((out / "meta.json").read_text())["last_scan"] is None
    assert ET.parse(feeds / "azure-ai-search.xml").getroot().findtext("./channel/link") == ""


def test_check_urls(project, http, monkeypatch, capsys):
    assert check_urls.main() == 0
    common.write_csv(project / "data" / "azure-ai-docs" / "changes" / "2026-10-05.csv", common.CHANGE_FIELDS,
                     [change(url="https://learn/ok"), change(url="https://learn/missing"), change(url="https://learn/down"),
                      change(url="")])
    from conftest import FakeResponse
    import requests
    http.add("HEAD", "https://learn/ok", FakeResponse(200))
    http.add("HEAD", "https://learn/down", requests.ConnectionError("dns failure"))
    assert check_urls.main() == 2
    out = capsys.readouterr().out
    assert "404  https://learn/missing" in out and "dns failure" in out and "1/3 sampled URLs resolved" in out


def test_rows_sort_by_utc_time(project, monkeypatch):
    rows = [change(commit_sha="early", commit_date="2026-10-06T01:00:00+00:00"),
            change(commit_sha="late", commit_date="2026-10-05T23:00:00-07:00")]  # 06:00 UTC on 6 Oct
    common.write_csv(project / "data" / "azure-ai-docs" / "changes" / "2026-10-05.csv", common.CHANGE_FIELDS, rows)
    assert run_main(build_site, monkeypatch) == 0
    out = json.loads((project / "site" / "data" / "changes.json").read_text())
    assert [r["commit_sha"] for r in out] == ["late", "early"]
    assert out[0]["commit_utc"] == "2026-10-06T06:00:00+00:00"
