from __future__ import annotations

import datetime as dt

import requests
import yaml

from conftest import FakeResponse, run_main, set_config
from tracker import common, discover

NOW = dt.datetime(2026, 10, 13, tzinfo=dt.timezone.utc)
API = "https://api.github.com"


def repo(name: str, pushed: str = "2026-10-10T00:00:00Z", archived: bool = False, owner: str = "MicrosoftDocs") -> dict:
    return {"name": name, "full_name": f"{owner}/{name}", "pushed_at": pushed, "archived": archived, "default_branch": "main"}


LEARN = '<html><head><meta name="original_content_git_url" content="https://github.com/MicrosoftDocs/azure-aks-docs-pr/blob/live/articles/aks/what-is-aks.md" /></head></html>'


def test_list_repos_paginates(http):
    http.add("GET", f"{API}/orgs/MicrosoftDocs/repos", FakeResponse(200, [repo(f"r{i}") for i in range(100)]),
             FakeResponse(200, [repo("last")]))
    assert len(discover.list_repos("MicrosoftDocs")) == 101
    assert [c["params"]["page"] for c in http.calls] == [1, 2]


def test_doc_config(http):
    http.add("GET", f"{API}/repos/o/a/contents/docfx.json", FakeResponse(200, {}))
    http.add("GET", f"{API}/repos/o/b/contents/.openpublishing.publish.config.json", FakeResponse(200, {}))
    assert discover.doc_config("o/a") == "docfx.json"
    assert discover.doc_config("o/b") == ".openpublishing.publish.config.json"
    assert discover.doc_config("o/c") == ""
    http.add("GET", f"{API}/repos/o/d/contents/docfx.json", FakeResponse(403))
    try:
        discover.doc_config("o/d")
    except requests.HTTPError:
        pass
    else:
        raise AssertionError("expected an HTTP error")


def test_learn_repo(http):
    http.add("GET", "https://learn/a", FakeResponse(200, LEARN))
    http.add("GET", "https://learn/b", FakeResponse(200, "<html>no tag</html>"))
    http.add("GET", "https://learn/c", FakeResponse(200, '<meta name="original_content_git_url">'))
    http.add("GET", "https://learn/d", FakeResponse(200, '<meta name="original_content_git_url" content="https://example.com/x">'))
    assert discover.learn_repo("https://learn/a") == "MicrosoftDocs/azure-aks-docs"
    assert discover.learn_repo("https://learn/b") == "" and discover.learn_repo("https://learn/c") == ""
    assert discover.learn_repo("https://learn/d") == ""


def test_matches_and_source_block():
    assert discover.matches("Azure-AI-Docs", ["azure-*"]) and not discover.matches("entra-docs", ["azure-*"])
    block = discover.source_block(repo("azure-aks-docs"), "docfx.json", False, "why")
    assert block["id"] == "azure-aks-docs" and not block["enabled"] and block["default_product"] == "Azure AKS"


def org_listing() -> list[dict]:
    return [repo("azure-ai-docs"), repo("azure-docs", pushed="2026-08-01T00:00:00Z"), repo("azure-compute-docs", archived=True),
            repo("azure-aks-docs", pushed="2026-07-01T00:00:00Z"), repo("azure-iot-docs"),  # configured but disabled
            repo("azure-new-docs"), repo("azure-new-docs-pr"), repo("azure-old-docs", pushed="2026-01-01T00:00:00Z"),
            repo("azure-archived-docs", archived=True), repo("entra-docs"), repo("azure-nodocs"), repo("azure-nopush", pushed=None)]


def test_discover_and_main(project, http, monkeypatch):
    set_config(project, lambda c: c["discovery"].update(learn_pages=[
        {"product": "AKS", "url": "https://learn/aks"}, {"product": "Broken", "url": "https://learn/broken"},
        {"product": "Configured", "url": "https://learn/conf"}]))
    http.add("GET", f"{API}/orgs/MicrosoftDocs/repos", FakeResponse(200, org_listing()))
    http.add("GET", "https://learn/aks", FakeResponse(200, LEARN))
    http.add("GET", "https://learn/broken", requests.ConnectionError("dns"))
    http.add("GET", "https://learn/conf", FakeResponse(200, LEARN.replace("azure-aks-docs-pr", "azure-ai-docs")))
    http.add("GET", f"{API}/repos/MicrosoftDocs/azure-new-docs/contents/docfx.json", FakeResponse(200, {}))
    http.add("GET", f"{API}/repos/MicrosoftDocs/azure-aks-docs/contents/docfx.json", FakeResponse(200, {}))
    report = discover.discover(common.load_config(), NOW)
    assert [c["repo"] for c in report["candidates"]] == ["MicrosoftDocs/azure-aks-docs", "MicrosoftDocs/azure-new-docs"]
    assert report["candidates"][0]["reason"] == "Learn page for AKS"
    stale = {s["repo"]: s["reason"] for s in report["stale"]}
    assert stale["MicrosoftDocs/azure-docs"] == "no push since 2026-08-01"
    assert stale["MicrosoftDocs/azure-compute-docs"] == "archived"
    assert "not found" in stale["MicrosoftDocs/azure-stack-docs"]
    assert [entry.get("error", "") != "" for entry in report["learn"]] == [False, True, False]
    assert report["learn"][2]["configured"] is True

    # main(): writes the report and discovered.yml; keeps an edited earlier block and an enabled one.
    (project / "config" / "discovered.yml").write_text(yaml.safe_dump({"sources": [
        {"id": "azure-new-docs", "repo": "MicrosoftDocs/azure-new-docs", "enabled": True, "default_product": "Edited"},
        {"id": "kept-docs", "repo": "MicrosoftDocs/kept-docs", "enabled": True},
        {"id": "dropped-docs", "repo": "MicrosoftDocs/dropped-docs", "enabled": False}]}))
    monkeypatch.setattr(discover, "utcnow", lambda: NOW)
    assert run_main(discover, monkeypatch) == 0
    blocks = {b["id"]: b for b in yaml.safe_load((project / "config" / "discovered.yml").read_text())["sources"]}
    assert set(blocks) == {"azure-aks-docs", "azure-new-docs", "kept-docs"}
    assert blocks["azure-new-docs"]["default_product"] == "Edited" and blocks["azure-aks-docs"]["enabled"] is False
    assert common.load_json(project / "data" / "discovery.json")["repos_listed"] == len(org_listing())


def test_main_fails_on_api_error(project, http, monkeypatch):
    http.add("GET", f"{API}/orgs/MicrosoftDocs/repos", FakeResponse(401))
    assert run_main(discover, monkeypatch) == 1


def test_discover_ignores_sources_outside_the_org(project, http):
    cfg = common.load_config()
    cfg["sources"] = [{"id": "x", "repo": "SharePoint/sp-dev-docs"}]
    cfg["discovery"] = {}
    http.add("GET", f"{API}/orgs/MicrosoftDocs/repos", FakeResponse(200, []))
    assert discover.discover(cfg, NOW)["stale"] == []
