from __future__ import annotations

import datetime as dt
import subprocess
import types

import pytest

from conftest import run_main, set_config
from tracker import common, scan

SRC = {"url_rules": [{"prefix": "articles/", "base": "https://learn.microsoft.com/en-us/azure/"}]}


def log_block(body: str, sha: str = "abc", subject: str = "s") -> str:
    return f"\x1e{sha}\x1fp\x1f2026-10-01T00:00:00Z\x1fA\x1f{subject}\n\n{body}"


def change(added: list[str], removed: list[str] | None = None, status: str = "Modified") -> dict:
    return {"status": status, "added": added, "removed": removed or [], "binary": False}


# ---------------------------------------------------------------- git helpers
def test_run_raises_on_failure(tmp_path):
    with pytest.raises(RuntimeError, match="failed"):
        scan.run(["git", "rev-parse", "HEAD"], cwd=tmp_path)
    assert scan.run(["git", "rev-parse", "HEAD"], cwd=tmp_path, check=False) == ""


def test_clone_arguments(monkeypatch, tmp_path):
    calls = []
    monkeypatch.setattr(scan, "run", lambda cmd, **kw: calls.append(cmd) or "")
    since = dt.datetime(2026, 10, 1, tzinfo=dt.timezone.utc)
    assert scan.clone("MicrosoftDocs/x", "main", since, tmp_path / "x") == tmp_path / "x"
    scan.clone("MicrosoftDocs/x", "", since, tmp_path / "y")
    assert ["--branch", "main"] == calls[0][4:6] and "--branch" not in calls[1]
    assert "--shallow-since=2026-08-17" in calls[0] and "--no-checkout" in calls[0]


def test_has_commit(fake_repo):
    head = scan.git(fake_repo, "rev-parse", "HEAD").strip()
    assert scan.has_commit(fake_repo, head)
    assert not scan.has_commit(fake_repo, "0" * 40)
    assert not scan.has_commit(fake_repo, "")


def test_cat_files(fake_repo, monkeypatch):
    assert scan.cat_files(fake_repo, []) == {}
    out = scan.cat_files(fake_repo, ["HEAD:articles/search/toc.yml", "HEAD:nope.md"])
    assert out["HEAD:articles/search/toc.yml"].startswith("- name:") and out["HEAD:nope.md"] is None
    fake = types.SimpleNamespace(stdout=b"abc ambiguous\n")
    monkeypatch.setattr(scan.subprocess, "run", lambda *a, **k: fake)
    assert scan.cat_files(fake_repo, ["abc:x"]) == {"abc:x": None}


def test_pr_by_commit_skips_merges_without_a_pr(monkeypatch):
    outputs = iter(["m1\x1fa b\x1fMerge pull request #7 from x/y\nm2\x1fa\x1fMerge pull request #8 from x/z\n"
                    "m3\x1fa b\x1fMerge branch 'main'\n", "c1\nc2\n"])
    monkeypatch.setattr(scan, "git", lambda *a, **k: next(outputs))
    assert scan.pr_by_commit(None, [], ["HEAD"]) == {"c1": "7", "c2": "7"}


# ---------------------------------------------------------------- page metadata and URLs
def test_parse_page_variants():
    md = "---\ntitle: T\ndescription: D\nms.service: s\nms.subservice: ss\nms.topic: how-to\nms.date: 2026-10-01\nauthor: a\n---\n# H\n"
    assert scan.parse_page("a.md", md) == {"title": "T", "description": "D", "ms_service": "s", "ms_subservice": "ss",
                                           "ms_topic": "how-to", "ms_date": "2026-10-01", "ms_author": "a"}
    assert scan.parse_page("a.md", None)["title"] == ""
    assert scan.parse_page("a.md", "# Heading only\ntext")["title"] == "Heading only"
    assert scan.parse_page("a.md", "no heading")["title"] == ""
    assert scan.parse_page("a.md", "---\ntitle: never closed")["title"] == ""
    assert scan.parse_page("a.md", "---\n- a list\n---\n# H1")["title"] == "H1"  # front matter that is not a mapping
    assert scan.parse_page("a.md", "---\n---\n# Empty")["title"] == "Empty"
    yml = "title: Hub\nsummary: S\nmetadata:\n  ms.service: azure-ai-search\n"
    assert scan.parse_page("index.yml", yml)["title"] == "Hub" and scan.parse_page("index.yml", yml)["description"] == "S"
    assert scan.parse_page("index.yml", "metadata:\n  title: Meta title\n")["title"] == "Meta title"
    assert scan.parse_page("toc.yml", "- name: x\n")["title"] == ""
    assert scan.parse_page("toc.yml", "")["title"] == ""
    broken = "---\ntitle: Broken: [unclosed\nms.topic: overview\n---\n"
    assert scan.parse_page("a.md", broken) | {} == scan.parse_page("a.md", broken)
    assert scan.parse_page("a.md", broken)["ms_topic"] == "overview"
    assert scan.parse_page("a.txt", "title: x")["title"] == ""


def test_learn_url():
    assert scan.learn_url("articles/search/index.yml", SRC) == "https://learn.microsoft.com/en-us/azure/search/"
    assert scan.learn_url("articles/search/vector.md", SRC) == "https://learn.microsoft.com/en-us/azure/search/vector"
    assert scan.learn_url("articles/search/includes/note.md", SRC) == ""
    assert scan.learn_url("articles/search/toc.yml", SRC) == ""
    assert scan.learn_url(".whatsnew.json", SRC) == ""
    assert scan.learn_url("other/x.md", SRC) == ""


# ---------------------------------------------------------------- diff parsing
def test_parse_log_keeps_content_lines_that_look_like_file_headers():
    raw = log_block("diff --git a/x.md b/x.md\nindex 1..2 100644\n--- a/x.md\n+++ b/x.md\n"
                    "@@ -5 +5 @@\n--- old rule text\n+++ new heading text\n\\ No newline at end of file\n")
    f = scan.parse_log(raw)[0]["files"][0]
    assert f["removed"] == ["-- old rule text"] and f["added"] == ["++ new heading text"]


def test_parse_log_statuses():
    raw = log_block(
        "diff --git a/a.md b/a.md\nnew file mode 100644\n--- /dev/null\n+++ b/a.md\n@@ -0,0 +1 @@\n+hello\n"
        "diff --git a/b.md b/b.md\ndeleted file mode 100644\n--- a/b.md\n+++ /dev/null\n@@ -1 +0,0 @@\n-bye\n"
        "diff --git a/c.md b/d.md\nsimilarity index 100%\nrename from c.md\nrename to d.md\n"
        "diff --git a/e.png b/e.png\nBinary files a/e.png and b/e.png differ\n"
        "diff --git oddly-formatted\n")
    files = scan.parse_log(raw)[0]["files"]
    assert [(f["status"], f["old"], f["path"]) for f in files] == [
        ("Added", "a.md", "a.md"), ("Deleted", "b.md", "b.md"), ("Renamed", "c.md", "d.md"),
        ("Modified", "e.png", "e.png"), ("Modified", "", "")]
    assert files[0]["added"] == ["hello"] and files[1]["removed"] == ["bye"] and files[3]["binary"]


# ---------------------------------------------------------------- classification
def test_ga_tag_needs_upper_case_word():
    tags, *_ = scan.classify(change(["The feature is now GA in all regions.", "x", "y", "z"]), "articles/a.md")
    assert "ga" in tags
    tags, *_ = scan.classify(change(["Set ga = load_config() before the call.", "x", "y", "z"]), "articles/a.md")
    assert "ga" not in tags


def test_noise_kinds():
    meta = scan.classify(change(["ms.date: 10/06/2026"], ["ms.date: 09/01/2026"]), "articles/a.md")
    assert meta == (["metadata-only"], 0, True, "", True, "Doc update")
    redirect = scan.classify(change(['{"redirections": [1]}'], []), ".openpublishing.redirection.json")
    assert redirect[0] == ["redirect"] and redirect[4]
    ws = scan.classify(change(["Some  text here. "], ["Some text here."]), "articles/a.md")
    assert ws[0] == ["whitespace-only"] and ws[4]
    link = scan.classify(change(["See [docs](new.md) and https://b"], ["See [docs](old.md) and https://a"]), "articles/a.md")
    assert link[0] == ["link-only"] and link[4]
    typo = scan.classify(change(["It uses language models.", "a", "b", "c"], ["It uses langauge models.", "a", "b", "d"]),
                         "articles/a.md")
    assert "minor-edit" in typo[0] and typo[4] and typo[1] <= 5
    big = scan.classify(change([f"new line {i}" for i in range(10)], [f"old text {i}" for i in range(10)]), "articles/a.md")
    assert not big[4] and big[5] == "Doc update"


def test_never_noise():
    # Keyword hit, what's-new file and TOC change are kept even when small.
    assert not scan.classify(change(["Now in preview."], ["Now in beta."]), "articles/a.md")[4]
    assert not scan.classify(change(["x"], ["y"]), "articles/whats-new.md")[4]
    assert not scan.classify(change(["- name: x"], ["- name: y"]), "articles/toc.yml")[4]
    added = scan.classify(change(["# Title", "Body text"], status="Added"), "articles/a.md")
    assert not added[4] and added[5] == "New"


def test_labels_and_tags():
    assert scan.classify(change(["This is a breaking change."], status="Added"), "a.md")[5] == "Breaking change"
    assert scan.classify(change(["x"], ["Feature is deprecated."], status="Deleted"), "a.md")[5] == "Retirement"
    renamed = scan.classify(change([], status="Renamed"), "a.md")
    assert renamed[0] == ["moved-page"] and renamed[5] == "Doc update"
    include = scan.classify(change(["text"], status="Added"), "articles/includes/x.md")
    assert include[0] == ["include"] and include[5] == "Doc update"
    sub = scan.classify(change(["a", "b", "c", "d"], ["e", "f", "g", "h"]), "a.md", "Retire the old page")
    assert "retirement" in sub[0]
    code = scan.classify(change(["```python", "x = 1", "```", "more"]), "a.md")
    assert "code-sample" in code[0]
    long = scan.classify(change(["**GA** of [feature](x.md)"] + ["preview " * 3] * 3, status="Added"), "a.md")
    assert long[3] == "GA of feature | preview preview preview"
    # A hit whose text is only markup (here a bare link) gives no evidence line.
    blank = scan.classify(change(["[](https://x/preview)", "generally available"], status="Added"), "a.md")
    assert blank[3] == "generally available"
    assert scan.change_label([]) == "Doc update"


def test_changed_chars():
    assert scan.changed_chars(["abc"], ["abd"]) == 1
    assert scan.changed_chars([], []) == 0


def test_change_summary():
    assert scan.change_summary("Add agentic retrieval GA article (#14010)", "Added", "T", 1, 0, []) == "Add agentic retrieval GA article"
    assert scan.change_summary("Update toc.yml", "Modified", "TOC", 1, 1, ["navigation"]).startswith("Navigation (TOC) updated")
    assert scan.change_summary("Update a.md", "Modified", "", 0, 0, ["metadata-only"]).startswith('Metadata-only edit to "untitled"')
    assert scan.change_summary("Update a.md", "Added", "T", 3, 0, ["new-page", "preview"]) == 'New page: "T" (+3/-0 lines); mentions preview'
    assert scan.change_summary("Learn Editor: Update a.md", "Modified", "T", 1, 1, []) == 'Edited: "T" (+1/-1 lines)'


# ---------------------------------------------------------------- end to end
def rows_of(project, repo_id="azure-ai-docs") -> list[dict]:
    return [r for p in sorted((project / "data" / repo_id / "changes").glob("*.csv")) for r in common.read_csv(p)]


def test_scan_end_to_end(project, fake_repo, monkeypatch):
    assert run_main(scan, monkeypatch, "--only", "azure-ai-docs", "--repo-path", f"azure-ai-docs={fake_repo}") == 0
    rows = rows_of(project)
    by = {(r["file_path"], r["commit_subject"]): r for r in rows}
    paths = {r["file_path"] for r in rows}
    # The initial commit is before start_date; media (pathspec), the PDF (binary) and .txt (include) are skipped.
    assert len(rows) == 13
    assert not paths & {"articles/search/media/x.png", "articles/search/diagram.pdf", "articles/search/notes.txt"}
    live = next(r for r in rows if r["file_path"] == "articles/ai-services/speech/live-interpreter.md")
    assert live["pr_number"] == "14851"  # from the merge commit
    renamed = next(r for r in rows if r["change_type"] == "Renamed")
    assert renamed["old_path"] == "articles/search/vector-search-overview.md" and renamed["pr_number"] == "15001"
    deleted = next(r for r in rows if r["change_type"] == "Deleted")
    assert deleted["title"] == "Legacy indexer" and deleted["url"].endswith("/search/legacy-indexer")
    assert next(r for r in rows if r["file_path"].endswith("toc.yml"))["title"] == "Table of contents (search)"
    noise = {by[("articles/search/semantic-ranking.md", s)]["tags"] for s in ("Fix formatting", "Fix link", "Fix typo")}
    assert noise == {"whitespace-only", "link-only", "minor-edit"}
    commits = common.read_csv(next((project / "data" / "azure-ai-docs" / "commits").glob("*.csv")))
    assert all(c["files_changed"] for c in commits if len(c["parents"].split()) == 1)
    state = common.load_state()["azure-ai-docs"]
    assert state["last_sha"] == scan.git(fake_repo, "rev-parse", "HEAD").strip()

    # Unchanged repository: nothing new.
    assert run_main(scan, monkeypatch, "--only", "azure-ai-docs", "--repo-path", f"azure-ai-docs={fake_repo}") == 0
    assert common.read_csv(project / "data" / "scans.csv")[-1]["status"] == "no-change"

    # A branch whose commit predates start_date, merged later: read via the watermark range, then dropped.
    subprocess.run(["bash", "-c", """
        set -e; git checkout -q -b old main~3
        echo 'old text' > articles/search/old.md; git add -A
        GIT_AUTHOR_DATE=2026-09-15T00:00:00Z GIT_COMMITTER_DATE=2026-09-15T00:00:00Z git commit -q -m 'Old branch work'
        git checkout -q main
        GIT_COMMITTER_DATE=2026-10-20T00:00:00Z git merge -q --no-ff old -m "Merge branch 'old'"
        echo 'Brand new page about preview features.' > articles/search/new.md; git add -A
        GIT_AUTHOR_DATE=2026-10-20T01:00:00Z GIT_COMMITTER_DATE=2026-10-20T01:00:00Z git commit -q -m 'Add new page'
        """], cwd=fake_repo, check=True)
    assert run_main(scan, monkeypatch, "--only", "azure-ai-docs", "--repo-path", f"azure-ai-docs={fake_repo}") == 0
    paths = {r["file_path"] for r in rows_of(project)}
    assert "articles/search/new.md" in paths and "articles/search/old.md" not in paths


def test_scan_falls_back_to_since_when_watermark_is_missing(project, fake_repo, monkeypatch):
    common.save_state({"azure-ai-docs": {"last_sha": "f" * 40, "last_sync": "2026-10-09T12:00:00+00:00"}})
    assert run_main(scan, monkeypatch, "--only", "azure-ai-docs", "--repo-path", f"azure-ai-docs={fake_repo}") == 0
    assert {r["commit_subject"] for r in rows_of(project)} == {
        "Rename vector search article and remove legacy indexer page (#15001)", "Update search TOC and ranker note",
        "Fix formatting", "Fix link", "Fix typo"}
    assert common.read_csv(project / "data" / "scans.csv")[-1]["since"] == "2026-10-09T12:00:00+00:00"
    # Re-running from scratch writes no duplicate rows.
    common.save_state({})
    before = len(rows_of(project))
    assert run_main(scan, monkeypatch, "--only", "azure-ai-docs", "--repo-path", f"azure-ai-docs={fake_repo}") == 0
    assert len(rows_of(project)) == 13 and before == 7


def test_scan_clones_when_no_local_path(project, fake_repo, monkeypatch):
    seen = {}

    def fake_clone(repo, branch, since, dest):
        seen.update(repo=repo, branch=branch)
        return fake_repo
    monkeypatch.setattr(scan, "clone", fake_clone)
    assert run_main(scan, monkeypatch, "--only", "azure-ai-docs") == 0
    assert seen == {"repo": "MicrosoftDocs/azure-ai-docs", "branch": "main"} and rows_of(project)


def test_scan_records_failures_and_exits_non_zero(project, monkeypatch):
    set_config(project, lambda c: c.update(sources=[s for s in c["sources"] if s["id"] in ("azure-ai-docs", "azure-docs")]))
    monkeypatch.setattr(scan, "clone", lambda *a: (_ for _ in ()).throw(RuntimeError("network down")))
    assert run_main(scan, monkeypatch) == 1
    logs = common.read_csv(project / "data" / "scans.csv")
    assert [entry["repo_id"] for entry in logs] == ["azure-ai-docs", "azure-docs"]
    assert all(entry["status"] == "error: network down" for entry in logs)


def test_scan_rejects_unknown_source(project, monkeypatch):
    assert run_main(scan, monkeypatch, "--only", "nope") == 2
