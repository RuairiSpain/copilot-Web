"""Unit and end-to-end tests for the tracker pipeline (run with `pytest -q` from caip-docs-tracker/)."""
from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from tracker import build_site, common, scan, summarise  # noqa: E402

SRC = {"url_rules": [{"prefix": "articles/", "base": "https://learn.microsoft.com/en-us/azure/"}]}


def log_block(body: str, sha: str = "abc", subject: str = "s") -> str:
    return f"\x1e{sha}\x1fp\x1f2026-10-01T00:00:00Z\x1fA\x1f{subject}\n\n{body}"


# ---------------------------------------------------------------- parsing and classification
def test_parse_log_keeps_content_lines_that_look_like_file_headers():
    raw = log_block("diff --git a/x.md b/x.md\nindex 1..2 100644\n--- a/x.md\n+++ b/x.md\n"
                    "@@ -5 +5 @@\n--- old rule text\n+++ new heading text\n")
    f = scan.parse_log(raw)[0]["files"][0]
    assert f["removed"] == ["-- old rule text"]
    assert f["added"] == ["++ new heading text"]


def test_parse_log_statuses():
    raw = log_block(
        "diff --git a/a.md b/a.md\nnew file mode 100644\n--- /dev/null\n+++ b/a.md\n@@ -0,0 +1 @@\n+hello\n"
        "diff --git a/b.md b/b.md\ndeleted file mode 100644\n--- a/b.md\n+++ /dev/null\n@@ -1 +0,0 @@\n-bye\n"
        "diff --git a/c.md b/d.md\nsimilarity index 100%\nrename from c.md\nrename to d.md\n")
    files = scan.parse_log(raw)[0]["files"]
    assert [(f["status"], f["old"], f["path"]) for f in files] == [
        ("Added", "a.md", "a.md"), ("Deleted", "b.md", "b.md"), ("Renamed", "c.md", "d.md")]
    assert files[0]["added"] == ["hello"] and files[1]["removed"] == ["bye"]


def file_change(added: list[str], removed: list[str] | None = None, status: str = "Modified") -> dict:
    return {"status": status, "added": added, "removed": removed or [], "binary": False}


def test_ga_tag_needs_upper_case_word():
    tags, *_ = scan.classify(file_change(["The feature is now GA in all regions.", "x", "y", "z"]), "articles/a.md")
    assert "ga" in tags
    tags, *_ = scan.classify(file_change(["Set ga = load_config() before the call.", "x", "y", "z"]), "articles/a.md")
    assert "ga" not in tags


def test_front_matter_only_edit_is_metadata_only():
    tags, score, meta_only, _ = scan.classify(file_change(["ms.date: 10/06/2026"], ["ms.date: 09/01/2026"]), "articles/a.md")
    assert meta_only and tags == ["metadata-only"] and score == 0


def test_learn_url():
    assert scan.learn_url("articles/search/index.yml", SRC) == "https://learn.microsoft.com/en-us/azure/search/"
    assert scan.learn_url("articles/search/vector.md", SRC) == "https://learn.microsoft.com/en-us/azure/search/vector"
    assert scan.learn_url("articles/search/includes/note.md", SRC) == ""
    assert scan.learn_url("articles/search/toc.yml", SRC) == ""
    assert scan.learn_url("other/x.md", SRC) == ""


def test_week_start_buckets_in_utc():
    # 01:00 Monday in Madrid is 23:00 Sunday UTC, so it belongs to the previous week.
    assert common.week_start("2026-10-05T01:00:00+02:00") == "2026-09-28"
    assert common.week_start("2026-10-05T09:00:00Z") == "2026-10-05"
    assert common.week_start("2026-10-11") == "2026-10-05"


# ---------------------------------------------------------------- pipeline
@pytest.fixture
def project(tmp_path, monkeypatch):
    """Point the pipeline at a throwaway data/state/site tree and a fake docs repo."""
    (tmp_path / "config").mkdir()
    (tmp_path / "config" / "sources.yml").write_text((ROOT / "config" / "sources.yml").read_text())
    monkeypatch.setattr(common, "CONFIG_PATH", tmp_path / "config" / "sources.yml")
    monkeypatch.setattr(common, "STATE_PATH", tmp_path / "state" / "state.json")
    for mod in (scan, summarise, build_site):
        monkeypatch.setattr(mod, "DATA_DIR", tmp_path / "data")
    monkeypatch.setattr(build_site, "SITE_DIR", tmp_path / "site")
    monkeypatch.delenv("GITHUB_TOKEN", raising=False)
    repo = tmp_path / "fake-repo"
    subprocess.run(["bash", str(ROOT / "tests" / "make_test_repo.sh"), str(repo)], check=True, capture_output=True)
    return tmp_path, repo


def run_scan(repo: Path, monkeypatch) -> None:
    monkeypatch.setattr(sys, "argv", ["scan", "--repo-path", f"azure-ai-docs={repo}"])
    scan.main()


def test_scan_end_to_end(project, monkeypatch):
    tmp, repo = project
    run_scan(repo, monkeypatch)
    rows = [r for p in sorted((tmp / "data" / "azure-ai-docs" / "changes").glob("*.csv")) for r in common.read_csv(p)]
    by_path = {r["file_path"]: r for r in rows}
    # The initial commit is before start_date and the PNG is excluded.
    assert len(rows) == 6 and "articles/search/media/x.png" not in by_path
    # A branch commit gets the PR number from the merge commit that brought it in.
    assert by_path["articles/ai-services/speech/live-interpreter.md"]["pr_number"] == "14851"
    assert by_path["articles/foundry/openai/models.md"]["pr_number"] == "14001"
    assert "retirement" in by_path["articles/foundry/openai/models.md"]["tags"]
    assert by_path["articles/search/vector-search-overview.md"]["metadata_only"] == "true"
    commits = common.read_csv(next((tmp / "data" / "azure-ai-docs" / "commits").glob("*.csv")))
    non_merge = [c for c in commits if len(c["parents"].split()) == 1]
    assert non_merge and all(c["files_changed"] for c in non_merge)

    # A second run with no new commits writes nothing new.
    run_scan(repo, monkeypatch)
    again = [r for p in (tmp / "data" / "azure-ai-docs" / "changes").glob("*.csv") for r in common.read_csv(p)]
    assert len(again) == 6
    assert common.read_csv(tmp / "data" / "scans.csv")[-1]["status"] == "no-change"


def test_scan_exits_non_zero_when_a_source_fails(project, monkeypatch):
    tmp, _ = project
    monkeypatch.setattr(sys, "argv", ["scan", "--repo-path", f"azure-ai-docs={tmp / 'missing'}"])
    with pytest.raises(SystemExit) as exc:
        scan.main()
    assert exc.value.code == 1
    assert common.read_csv(tmp / "data" / "scans.csv")[-1]["status"].startswith("error")


def test_summary_retries_after_model_failure(project, monkeypatch):
    tmp, repo = project
    run_scan(repo, monkeypatch)
    cfg = json.loads(json.dumps(common.load_config()))
    cfg["summary"]["provider"] = "github-models"
    monkeypatch.setattr(summarise, "load_config", lambda: cfg)

    monkeypatch.setattr(summarise, "call_llm", lambda *a: None)
    summarise.main()
    week = json.loads((tmp / "data" / "azure-ai-docs" / "summaries" / "2026-09-28.json").read_text())
    assert week["Azure OpenAI"]["provider"] == "rule-based"

    bullet = {"headline": "h", "detail": "d", "impact": "High", "urls": []}
    monkeypatch.setattr(summarise, "call_llm", lambda *a: [bullet])
    summarise.main()
    week = json.loads((tmp / "data" / "azure-ai-docs" / "summaries" / "2026-09-28.json").read_text())
    assert week["Azure OpenAI"]["provider"] == "github-models"
    assert week["Azure OpenAI"]["bullets"] == [bullet]
    # In the week of 5 October, Azure AI Search only had a metadata-only edit: no bullets, no model call.
    later = json.loads((tmp / "data" / "azure-ai-docs" / "summaries" / "2026-10-05.json").read_text())
    assert later["Azure AI Search"]["provider"] == "none" and later["Azure AI Search"]["bullets"] == []


def test_build_site_merges_a_product_fed_by_two_repos(project, monkeypatch):
    tmp, _ = project
    cfg = common.load_config()
    cfg["sources"].append({"id": "azure-docs", "repo": "MicrosoftDocs/azure-docs"})
    monkeypatch.setattr(build_site, "load_config", lambda: cfg)
    for repo_id, headline in (("azure-ai-docs", "a"), ("azure-docs", "b")):
        d = tmp / "data" / repo_id / "summaries"
        d.mkdir(parents=True)
        (d / "2026-10-05.json").write_text(json.dumps({"Azure OpenAI": {
            "row_count": 2, "generated_at": "2026-10-09T18:00:00+00:00", "provider": "github-models",
            "bullets": [{"headline": headline, "detail": "", "impact": "Low", "urls": []}]}}))
    build_site.main()
    merged = json.loads((tmp / "site" / "data" / "summaries.json").read_text())["Azure OpenAI"]["2026-10-05"]
    assert merged["row_count"] == 4 and [b["headline"] for b in merged["bullets"]] == ["a", "b"]
