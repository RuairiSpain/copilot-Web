from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

import pytest

from tools.worktrees import GitError, WorktreeManager


def write(path: Path, text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text)


class TestRepository:
    def test_ensure_repo_creates_main_with_a_first_commit(self, manager: WorktreeManager) -> None:
        manager.ensure_repo()
        assert (manager.repo / ".git").exists()
        assert "Initial commit" in manager.log()
        assert ".worktrees/" in (manager.repo / ".gitignore").read_text()

    def test_ensure_repo_is_idempotent(self, manager: WorktreeManager) -> None:
        manager.ensure_repo()
        manager.ensure_repo()
        assert manager.log().count("Initial commit") == 1

    def test_git_errors_are_raised_with_the_reason(self, manager: WorktreeManager) -> None:
        manager.ensure_repo()
        with pytest.raises(GitError, match="bogus"):
            manager._git("bogus")


class TestWorktrees:
    def test_add_creates_a_branch_and_a_folder(self, manager: WorktreeManager) -> None:
        path = manager.add("planner")
        assert path == manager.repo / ".worktrees" / "planner"
        assert path.is_dir()
        branches = {info.branch for info in manager.list()}
        assert {"main", "agent/planner"} <= branches

    def test_add_twice_returns_the_same_folder(self, manager: WorktreeManager) -> None:
        assert manager.add("writer") == manager.add("writer")

    @pytest.mark.parametrize("name", ["", "Planner", "../x", "a b", "1x", "x" * 40])
    def test_invalid_role_names_are_refused(self, manager: WorktreeManager, name: str) -> None:
        with pytest.raises(ValueError, match="Invalid role name"):
            manager.add(name)

    def test_remove_keeps_the_branch(self, manager: WorktreeManager) -> None:
        path = manager.add("tester")
        manager.remove("tester")
        assert not path.exists()
        shown = manager._git("show-ref", "--verify", "refs/heads/agent/tester", check=False)
        assert shown.returncode == 0

    def test_add_after_remove_reuses_the_branch(self, manager: WorktreeManager) -> None:
        manager.add("tester")
        manager.remove("tester")
        assert manager.add("tester").is_dir()

    def test_status_reports_the_branch(self, manager: WorktreeManager) -> None:
        manager.add("writer")
        assert "agent/writer" in manager.status("writer")


class TestSharingWork:
    def test_publish_merges_into_main(self, manager: WorktreeManager) -> None:
        planner = manager.add("planner")
        write(planner / "docs" / "tasks.md", "- [ ] task")
        result = manager.publish("planner", "Add the task list")
        assert result.ok
        assert (manager.repo / "docs" / "tasks.md").read_text() == "- [ ] task"
        assert "Merge agent/planner" in manager.log()

    def test_publish_with_nothing_new_still_succeeds(self, manager: WorktreeManager) -> None:
        manager.add("planner")
        assert manager.publish("planner", "Nothing").ok

    def test_commit_reports_whether_anything_changed(self, manager: WorktreeManager) -> None:
        planner = manager.add("planner")
        assert manager.commit("planner", "empty") is False
        write(planner / "docs" / "a.md", "x")
        assert manager.commit("planner", "add a") is True

    def test_refresh_brings_in_other_roles_work(self, manager: WorktreeManager) -> None:
        planner = manager.add("planner")
        writer = manager.add("writer")
        write(planner / "docs" / "tasks.md", "plan")
        manager.publish("planner", "plan")
        assert not (writer / "docs" / "tasks.md").exists()
        assert manager.refresh("writer").ok
        assert (writer / "docs" / "tasks.md").read_text() == "plan"

    def test_refresh_saves_uncommitted_work_first(self, manager: WorktreeManager) -> None:
        planner = manager.add("planner")
        writer = manager.add("writer")
        write(planner / "docs" / "tasks.md", "plan")
        manager.publish("planner", "plan")
        write(writer / "src" / "a.py", "x = 1")  # uncommitted
        assert manager.refresh("writer").ok
        assert (writer / "src" / "a.py").exists()

    def test_conflicts_are_reported_and_main_stays_clean(self, manager: WorktreeManager) -> None:
        planner = manager.add("planner")
        writer = manager.add("writer")
        write(planner / "README.md", "planner version\n")
        write(writer / "README.md", "writer version\n")
        assert manager.publish("planner", "planner readme").ok
        result = manager.publish("writer", "writer readme")
        assert not result.ok
        assert result.conflicts == ("README.md",)
        assert manager._git("status", "--porcelain").stdout == ""
        assert not (manager.repo / ".git" / "MERGE_HEAD").exists()
        assert (manager.repo / "README.md").read_text() == "planner version\n"

    def test_concurrent_publishes_do_not_corrupt_main(self, manager: WorktreeManager) -> None:
        paths = {role: manager.add(role) for role in ("planner", "writer", "tester")}
        write(paths["planner"] / "docs" / "tasks.md", "a")
        write(paths["writer"] / "src" / "a.py", "b")
        write(paths["tester"] / "tests" / "test_a.py", "c")
        with ThreadPoolExecutor(max_workers=3) as pool:
            results = list(pool.map(lambda r: manager.publish(r, f"publish {r}"), paths))
        assert all(r.ok for r in results)
        for relative in ("docs/tasks.md", "src/a.py", "tests/test_a.py"):
            assert (manager.repo / relative).exists()
        assert manager._git("status", "--porcelain").stdout == ""
