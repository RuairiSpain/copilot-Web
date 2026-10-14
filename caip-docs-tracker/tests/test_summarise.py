from __future__ import annotations

import json

import requests

from conftest import FakeResponse, run_main, set_config
from tracker import common, summarise

ROW = {"title": "T", "url": "https://learn/x", "commit_url": "https://gh/c", "change_type": "Added", "tags": "new-page",
       "change_label": "New", "change_summary": "Add x", "key_lines": "", "lines_added": 3, "lines_removed": 0,
       "importance": 40, "metadata_only": "false", "noise": "false", "file_path": "a.md"}


def reply(content: str) -> FakeResponse:
    return FakeResponse(200, {"choices": [{"message": {"content": content}}]})


def test_candidate_rows_drop_noise_and_sort():
    rows = [dict(ROW, importance=10), dict(ROW, importance=50), dict(ROW, noise="true"),
            dict(ROW, metadata_only="true", noise=""), dict(ROW, tags="minor-edit", noise="")]
    assert [r["importance"] for r in summarise.candidate_rows(rows, 5)] == [50, 10]
    assert len(summarise.candidate_rows(rows, 1)) == 1


def test_call_llm_github_models(http, monkeypatch):
    assert summarise.call_llm({"provider": "github-models"}, "P", "w", [ROW]) is None  # no token
    monkeypatch.setenv("GITHUB_TOKEN", "t")
    body = {"bullets": [
        {"headline": "H", "detail": "D", "impact": "High", "label": "GA", "urls": ["https://learn/x", "https://evil", 3]},
        {"headline": "H2", "impact": ["bad"], "label": ["bad"], "urls": "not-a-list"},
        "junk"]}
    http.add("POST", "https://models.github.ai/", reply("```json\n" + json.dumps(body) + "\n```"))
    out = summarise.call_llm({"provider": "github-models", "model": "m"}, "P", "w", [ROW])
    assert out == [{"headline": "H", "detail": "D", "impact": "High", "label": "GA", "urls": ["https://learn/x"]},
                   {"headline": "H2", "detail": "", "impact": "Medium", "label": "Doc update", "urls": []}]
    sent = http.calls[-1]
    assert sent["json"]["model"] == "m" and sent["headers"]["Authorization"] == "Bearer t"


def test_call_llm_azure_openai(http, monkeypatch):
    cfg = {"provider": "azure-openai"}
    assert summarise.call_llm(cfg, "P", "w", [ROW]) is None  # not configured
    for k, v in {"AZURE_OPENAI_ENDPOINT": "https://aoai/", "AZURE_OPENAI_API_KEY": "k", "AZURE_OPENAI_DEPLOYMENT": "d"}.items():
        monkeypatch.setenv(k, v)
    http.add("POST", "https://aoai/openai/deployments/d/", reply('{"bullets": []}'))
    assert summarise.call_llm(cfg, "P", "w", [ROW]) == []
    assert http.calls[-1]["headers"]["api-key"] == "k"
    assert summarise.call_llm({"provider": "other"}, "P", "w", [ROW]) is None


def test_call_llm_bad_responses(http, monkeypatch):
    monkeypatch.setenv("GITHUB_TOKEN", "t")
    cfg = {"provider": "github-models"}
    http.add("POST", "https://models.github.ai/", FakeResponse(401, "no"), reply("not json"), reply('{"bullets": {"a": 1}}'),
             requests.ConnectionError("down"))
    assert summarise.call_llm(cfg, "P", "w", [ROW]) is None   # HTTP error
    assert summarise.call_llm(cfg, "P", "w", [ROW]) is None   # not JSON
    assert summarise.call_llm(cfg, "P", "w", [ROW]) is None   # bullets not a list
    assert summarise.call_llm(cfg, "P", "w", [ROW]) is None   # network error


def test_rule_based():
    out = summarise.rule_based([dict(ROW, importance=60, key_lines="k", url=""), dict(ROW, importance=30, change_label=""),
                                dict(ROW, importance=5, title="")], 8)
    assert [b["impact"] for b in out] == ["High", "Medium", "Low"]
    assert out[0]["detail"] == 'Add x. Evidence: "k"' and out[0]["urls"] == ["https://gh/c"]
    assert out[1]["label"] == "Doc update" and out[2]["headline"] == "a.md"


def write_week(project, week: str, rows: list[dict], repo="azure-ai-docs"):
    common.write_csv(project / "data" / repo / "changes" / f"{week}.csv", common.CHANGE_FIELDS, rows)


def summary(project, week: str, repo="azure-ai-docs") -> dict:
    return json.loads((project / "data" / repo / "summaries" / f"{week}.json").read_text())


def test_main_caches_retries_and_skips(project, http, monkeypatch):
    rows = [dict(ROW, product="Azure OpenAI", importance=60), dict(ROW, product="Azure OpenAI", importance=10),
            dict(ROW, product="Azure AI Search", noise="true")]
    write_week(project, "2026-10-05", rows)
    run_main(summarise, monkeypatch)  # no token: rule-based
    s = summary(project, "2026-10-05")
    assert s["Azure OpenAI"]["provider"] == "rule-based" and [b["impact"] for b in s["Azure OpenAI"]["bullets"]] == ["High", "Low"]
    assert s["Azure AI Search"] == {**s["Azure AI Search"], "provider": "none", "bullets": [], "row_count": 1}

    stamp = (project / "data" / "azure-ai-docs" / "summaries" / "2026-10-05.json").stat().st_mtime_ns
    run_main(summarise, monkeypatch)  # still no token: retry fails again, file left untouched
    assert (project / "data" / "azure-ai-docs" / "summaries" / "2026-10-05.json").stat().st_mtime_ns == stamp

    monkeypatch.setenv("GITHUB_TOKEN", "t")
    http.add("POST", "https://models.github.ai/", reply('{"bullets": [{"headline": "L", "impact": "Low"}, {"headline": "H", "impact": "High"}]}'))
    run_main(summarise, monkeypatch)  # model available: the rule-based week is replaced
    s = summary(project, "2026-10-05")
    assert s["Azure OpenAI"]["provider"] == "github-models" and [b["headline"] for b in s["Azure OpenAI"]["bullets"]] == ["H", "L"]
    calls = len(http.calls)
    run_main(summarise, monkeypatch)  # nothing changed: no model call
    assert len(http.calls) == calls

    set_config(project, lambda c: c["summary"].update(provider="none"))
    write_week(project, "2026-10-12", [dict(ROW, product="Azure OpenAI")])
    run_main(summarise, monkeypatch)
    run_main(summarise, monkeypatch)  # provider none: a rule-based summary is final
    assert summary(project, "2026-10-12")["Azure OpenAI"]["provider"] == "rule-based"


def test_large_edit_rows_send_the_whole_page(project, http, monkeypatch):
    monkeypatch.setenv("GITHUB_TOKEN", "t")
    http.add("POST", "https://models.github.ai/", reply('{"bullets": []}'))
    rows = []
    for i in range(7):
        r = dict(ROW, tags="large-edit", repo_id="azure-ai-docs", commit_sha=f"sha{i}", file_path=f"a{i}.md")
        path = common.page_cache_path("azure-ai-docs", f"sha{i}", f"a{i}.md")
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("The page now says " + "x" * 10_000)
        rows.append(r)
    rows.append(dict(ROW, tags="large-edit", repo_id="azure-ai-docs", commit_sha="nocache", file_path="z.md"))
    summarise.call_llm({"provider": "github-models"}, "P", "w", rows)
    sent = json.loads(http.calls[-1]["json"]["messages"][1]["content"].split("Changes (JSON):\n", 1)[1])
    pages = [r.get("page", "") for r in sent]
    assert sum(1 for p in pages if p) == summarise.MAX_PAGES_PER_CALL
    assert all(len(p) == summarise.PAGE_PROMPT_CHARS for p in pages if p) and pages[-1] == ""
    assert "large edit" in http.calls[-1]["json"]["messages"][0]["content"]
    monkeypatch.setenv("TRACKER_LARGE_EDITS", "off")
    assert summarise.page_text(rows[0]) == ""
    assert summarise.page_text(dict(ROW)) == ""


def test_rule_based_large_edit_mentions_the_page():
    out = summarise.rule_based([dict(ROW, tags="large-edit", description="How to deploy agents")], 8)
    assert out[0]["detail"].endswith("The page now covers: How to deploy agents")
    assert "covers" not in summarise.rule_based([dict(ROW, tags="large-edit", description="")], 8)[0]["detail"]


def test_retries_are_limited_to_recent_weeks_and_stop_after_failures(project, http, monkeypatch):
    set_config(project, lambda c: c["summary"].update(retry_weeks=2))
    this_week = common.week_start(common.utcnow())
    old_week = "2026-01-05"
    products = ["A", "B", "C", "D", "E"]
    write_week(project, this_week, [dict(ROW, product=p) for p in products])
    write_week(project, old_week, [dict(ROW, product="Old")])
    run_main(summarise, monkeypatch)  # no token: everything rule-based
    monkeypatch.setenv("GITHUB_TOKEN", "t")
    http.add("POST", "https://models.github.ai/", FakeResponse(401))
    run_main(summarise, monkeypatch)
    # The old week is outside the retry window; this week stops after MAX_MODEL_FAILURES failed calls.
    assert len(http.calls) == summarise.MAX_MODEL_FAILURES
    assert summary(project, old_week)["Old"]["provider"] == "rule-based"
    http.routes.clear()
    http.add("POST", "https://models.github.ai/", reply('{"bullets": []}'))
    run_main(summarise, monkeypatch)
    assert {summary(project, this_week)[p]["provider"] for p in products} == {"github-models"}
