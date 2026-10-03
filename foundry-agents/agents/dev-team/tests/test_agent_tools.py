from __future__ import annotations

from pathlib import Path
from typing import Any

import pytest

from tools.agent_tools import TeamContext, build_role_tools

from .conftest import call_tool

BASE_TOOLS = {
    "run_command",
    "read_file",
    "write_file",
    "list_files",
    "git_status",
    "git_commit",
    "publish_work",
    "refresh_from_main",
}


def by_name(role: str, ctx: TeamContext) -> dict[str, Any]:
    return {t.name: t for t in build_role_tools(role, ctx)}


class TestToolSets:
    @pytest.mark.parametrize("role", ["lead", "planner"])
    def test_non_code_roles_get_the_base_tools(self, role: str, ctx: TeamContext) -> None:
        assert set(by_name(role, ctx)) == BASE_TOOLS

    @pytest.mark.parametrize("role", ["writer", "tester"])
    def test_code_roles_also_get_a_test_runner(self, role: str, ctx: TeamContext) -> None:
        assert set(by_name(role, ctx)) == BASE_TOOLS | {"run_tests"}

    def test_unknown_roles_are_refused(self, ctx: TeamContext) -> None:
        with pytest.raises(ValueError, match="Unknown role"):
            build_role_tools("intern", ctx)

    def test_the_write_tool_advertises_the_roles_paths(self, ctx: TeamContext) -> None:
        assert "src/" in by_name("writer", ctx)["write_file"].description
        assert "tests/" in by_name("tester", ctx)["write_file"].description

    def test_tool_schemas_expose_the_real_parameters(self, ctx: TeamContext) -> None:
        tools = by_name("writer", ctx)
        assert set(tools["write_file"].parameters()["properties"]) == {"path", "content"}
        assert set(tools["run_command"].parameters()["properties"]) == {"command"}
        assert "args" in tools["run_tests"].parameters()["properties"]


class TestFileTools:
    async def test_write_read_and_list(self, ctx: TeamContext) -> None:
        tools = by_name("writer", ctx)
        assert "Wrote src/pkg/models.py" in await call_tool(
            tools["write_file"], path="src/pkg/models.py", content="x = 1\n"
        )
        assert await call_tool(tools["read_file"], path="src/pkg/models.py") == "x = 1\n"
        assert "src/pkg/models.py" in await call_tool(tools["list_files"])

    async def test_ownership_errors_come_back_as_text(self, ctx: TeamContext) -> None:
        tools = by_name("writer", ctx)
        reply = await call_tool(tools["write_file"], path="tests/test_x.py", content="x")
        assert reply.startswith("ERROR:") and "tester" in reply

    async def test_path_escape_comes_back_as_text(self, ctx: TeamContext) -> None:
        reply = await call_tool(
            by_name("writer", ctx)["write_file"], path="../evil.py", content="x"
        )
        assert reply.startswith("ERROR:")

    async def test_each_role_works_in_its_own_worktree(self, ctx: TeamContext) -> None:
        await call_tool(by_name("writer", ctx)["write_file"], path="src/a.py", content="x")
        assert (ctx.manager.path_for("writer") / "src" / "a.py").exists()
        assert not (ctx.manager.path_for("tester") / "src" / "a.py").exists()


class TestCommandTools:
    async def test_blocked_commands_come_back_as_text(self, ctx: TeamContext) -> None:
        reply = await call_tool(
            by_name("writer", ctx)["run_command"], command="git push origin main"
        )
        assert reply.startswith("ERROR:") and "not allowed" in reply

    async def test_allowed_commands_run_in_the_roles_worktree(self, ctx: TeamContext) -> None:
        reply = await call_tool(
            by_name("writer", ctx)["run_command"], command="git status --short --branch"
        )
        assert reply.startswith("exit code: 0") and "agent/writer" in reply

    async def test_git_status_tool(self, ctx: TeamContext) -> None:
        assert "agent/planner" in await call_tool(by_name("planner", ctx)["git_status"])

    async def test_run_tests_runs_pytest_in_the_host(self, ctx: TeamContext) -> None:
        tester = by_name("tester", ctx)
        await call_tool(
            tester["write_file"],
            path="tests/test_ok.py",
            content="def test_ok():\n    assert 1 + 1 == 2\n",
        )
        reply = await call_tool(tester["run_tests"])
        assert "exit code: 0" in reply and "1 passed" in reply

    async def test_run_tests_reports_failures(self, ctx: TeamContext) -> None:
        tester = by_name("tester", ctx)
        await call_tool(
            tester["write_file"],
            path="tests/test_bad.py",
            content="def test_bad():\n    assert 1 == 2\n",
        )
        reply = await call_tool(tester["run_tests"])
        assert "exit code: 1" in reply and "1 failed" in reply


class TestGitTools:
    async def test_commit_and_empty_commit(self, ctx: TeamContext) -> None:
        tools = by_name("planner", ctx)
        await call_tool(tools["write_file"], path="docs/tasks.md", content="- [ ] a")
        assert await call_tool(tools["git_commit"], message="Add tasks") == "Committed."
        assert await call_tool(tools["git_commit"], message="Again") == "Nothing to commit."

    async def test_empty_message_is_refused(self, ctx: TeamContext) -> None:
        assert (await call_tool(by_name("planner", ctx)["git_commit"], message="  ")).startswith(
            "ERROR:"
        )

    async def test_publish_then_refresh_shares_work_between_roles(self, ctx: TeamContext) -> None:
        planner, writer = by_name("planner", ctx), by_name("writer", ctx)
        await call_tool(planner["write_file"], path="docs/tasks.md", content="plan")
        assert "Published to main" in await call_tool(
            planner["publish_work"], message="Add the plan"
        )
        assert (ctx.manager.repo / "docs" / "tasks.md").exists()
        assert "Up to date with main" in await call_tool(writer["refresh_from_main"])
        assert await call_tool(writer["read_file"], path="docs/tasks.md") == "plan"

    async def test_conflicts_are_explained_to_the_model(self, ctx: TeamContext) -> None:
        # Ownership prevents this through the tools, so create the clash on disk directly.
        planner, writer = by_name("planner", ctx), by_name("writer", ctx)
        for role in ("planner", "writer"):
            path: Path = ctx.worktree(role) / "NOTES.md"
            path.write_text(f"{role}\n")
        assert "Published" in await call_tool(planner["publish_work"], message="planner notes")
        reply = await call_tool(writer["publish_work"], message="writer notes")
        assert reply.startswith("MERGE CONFLICT in NOTES.md")
