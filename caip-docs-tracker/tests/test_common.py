from __future__ import annotations

import datetime as dt

from conftest import FakeResponse
from tracker import common


def test_load_yaml_and_json_missing_or_empty(tmp_path):
    assert common.load_yaml(tmp_path / "nope.yml") == {}
    (tmp_path / "empty.yml").write_text("")
    assert common.load_yaml(tmp_path / "empty.yml") == {}
    assert common.load_json(tmp_path / "nope.json") == {}
    (tmp_path / "empty.json").write_text("")
    assert common.load_json(tmp_path / "empty.json") == {}
    common.save_json(tmp_path / "sub" / "x.json", {"b": 1, "a": 2})
    assert common.load_json(tmp_path / "sub" / "x.json") == {"a": 2, "b": 1}


def test_state_round_trip(project):
    assert common.load_state() == {}
    common.save_state({"x": {"last_sha": "abc"}})
    assert common.load_state() == {"x": {"last_sha": "abc"}}


def test_enabled_sources_merges_discovered(project):
    (project / "config" / "discovered.yml").write_text(
        "sources:\n- {id: new-docs, repo: MicrosoftDocs/new-docs, enabled: true}\n"
        "- {id: off-docs, repo: MicrosoftDocs/off-docs, enabled: false}\n"
        "- {id: azure-ai-docs, repo: dup, enabled: true}\n")
    ids = [s["id"] for s in common.enabled_sources(common.load_config())]
    assert "new-docs" in ids and "off-docs" not in ids and ids.count("azure-ai-docs") == 1
    assert "azure-docs-sdk-dotnet" not in ids  # enabled: false in sources.yml


def test_dates():
    assert common.parse_iso("2026-10-01T10:00:00").tzinfo == dt.timezone.utc
    assert common.week_start(dt.date(2026, 10, 8)) == "2026-10-05"
    assert common.week_start(dt.datetime(2026, 10, 5, 1, tzinfo=dt.timezone(dt.timedelta(hours=2)))) == "2026-09-28"
    assert common.week_start("2026-10-05T01:00:00+02:00") == "2026-09-28"
    assert common.week_start("2026-10-11") == "2026-10-05"
    assert common.weeks_between("2026-10-01", "2026-10-13") == ["2026-09-28", "2026-10-05", "2026-10-12"]
    assert common.utcnow().microsecond == 0


def test_path_matches_and_prefix():
    assert common.path_matches("articles/a/b.md", ["articles/**/*.md"])
    assert common.path_matches("articles/b.md", ["articles/**/*.md"])
    assert not common.path_matches("other/b.md", ["articles/**/*.md"])
    assert not common.path_matches("x", None)
    assert common.first_prefix("a/b", [{"prefix": "a/", "v": 1}], "v") == (1, {"prefix": "a/", "v": 1})
    assert common.first_prefix("z", [{"prefix": "a/", "v": 1}], "v", "d") == ("d", None)


def test_humanize_and_slug():
    assert common.humanize("api-management") == "API Management"
    assert common.humanize("-virtual_machines-") == "Virtual Machines"
    assert common.slug("Foundry Tools (AI services)") == "foundry-tools-ai-services"
    assert common.slug("!!!") == "x"


def test_resolve_product_order():
    src = {"products": [{"prefix": "articles/search/", "product": "Azure AI Search"}],
           "folder_products": {"prefix": "articles/", "name_prefix": "Azure ", "names": {"api-management": "API Management"}},
           "default_product": "Other"}
    cfg = {"ms_service_products": {"azure-functions": "Azure Functions"}}
    assert common.resolve_product("articles/search/x.md", src, "azure-functions", cfg) == "Azure AI Search"
    assert common.resolve_product("articles/foo/x.md", src, "azure-functions", cfg) == "Azure Functions"
    assert common.resolve_product("articles/api-management/x.md", src, "", cfg) == "API Management"
    assert common.resolve_product("articles/storage/blobs/x.md", src) == "Azure Storage"
    assert common.resolve_product("articles/index.yml", src) == "Other"   # file directly under the prefix
    assert common.resolve_product("other/x.md", src) == "Other"
    assert common.resolve_product("x.md", {}) == "Other"
    assert common.resolve_product("a/x.md", {"folder_products": {"prefix": ""}}) == "A"  # empty prefix, no names


def test_product_aliases_and_matching():
    aliases = common.product_aliases({"product_aliases": {"API Management": ["APIM", "Azure API Management"], "Azure SQL": None}})
    assert aliases == {"api management": "API Management", "apim": "API Management",
                       "azure api management": "API Management", "azure sql": "Azure SQL"}
    assert common.product_aliases({}) == {}
    assert common.products_matching(aliases, ["Azure API Management", "Compute"], "") == ["API Management"]
    assert common.products_matching(aliases, [], "Retiring Azure SQL feature in APIM") == ["API Management", "Azure SQL"]
    assert common.products_matching(aliases, [], "Azure SQL-based tool") == []  # not a whole phrase


def test_csv_round_trip_and_schema_migration(tmp_path):
    p = tmp_path / "d" / "x.csv"
    common.append_csv(p, ["a", "b"], [])
    assert not p.exists()
    common.append_csv(p, ["a", "b"], [{"a": 1, "b": 2}])
    common.append_csv(p, ["a", "b"], [{"a": 3, "b": 4, "c": 9}])
    assert common.read_csv(p) == [{"a": "1", "b": "2"}, {"a": "3", "b": "4"}]
    common.append_csv(p, ["a", "b", "c"], [{"a": 5, "b": 6, "c": 7}])  # new column: file is rewritten
    assert common.read_csv(p) == [{"a": "1", "b": "2", "c": ""}, {"a": "3", "b": "4", "c": ""}, {"a": "5", "b": "6", "c": "7"}]
    assert common.read_csv(tmp_path / "missing.csv") == []


def test_flags(monkeypatch):
    assert common.is_true("True") and common.is_true(1) and not common.is_true("") and not common.is_true(None)
    monkeypatch.setenv("X_FLAG", "yes")
    assert common.env_flag("X_FLAG")
    monkeypatch.delenv("X_FLAG")
    assert not common.env_flag("X_FLAG")


def test_http_request_retries(http):
    http.add("GET", "https://a/", FakeResponse(429, headers={"Retry-After": "2"}), FakeResponse(503), FakeResponse(200, "ok"))
    assert common.http_request("GET", "https://a/x").text == "ok"
    assert len(http.calls) == 3 and http.calls[0]["headers"]["User-Agent"].startswith("caip-docs-tracker")
    http.add("GET", "https://b/", FakeResponse(500))
    assert common.http_request("GET", "https://b/", attempts=2).status_code == 500
    http.add("GET", "https://c/", FakeResponse(404))
    assert common.http_request("GET", "https://c/", headers={"X": "1"}).status_code == 404
    assert http.calls[-1]["headers"]["X"] == "1"


def test_github_headers(monkeypatch):
    monkeypatch.delenv("GITHUB_TOKEN", raising=False)
    assert "Authorization" not in common.github_headers()
    monkeypatch.setenv("GITHUB_TOKEN", "t")
    assert common.github_headers()["Authorization"] == "Bearer t"
