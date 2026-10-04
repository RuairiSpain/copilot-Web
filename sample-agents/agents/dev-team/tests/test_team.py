from __future__ import annotations

from pathlib import Path
from types import SimpleNamespace
from typing import Any

import pytest
from agent_framework import Message

import team as team_module
from intake.brief import ProjectBrief
from team import (
    PARTICIPANTS,
    DevTeam,
    build_participants,
    build_workflow,
    extract_final_text,
    load_prompt,
    team_task,
)
from tools.agent_tools import TeamContext

from .conftest import FakeChatClient


@pytest.fixture
def brief(brief_dict: dict[str, Any]) -> ProjectBrief:
    return ProjectBrief.model_validate(brief_dict)


def tool_names(agent: Any) -> set[str]:
    return {t.name for t in agent.default_options["tools"]}


class TestPrompts:
    @pytest.mark.parametrize("name", ["lead", "manager", "planner", "writer", "tester"])
    def test_every_prompt_exists_and_has_substance(self, name: str) -> None:
        assert len(load_prompt(name)) > 200

    def test_each_specialist_states_what_it_may_write(self) -> None:
        assert "docs/tasks.md" in load_prompt("planner")
        assert "src/" in load_prompt("writer") and "pyproject.toml" in load_prompt("writer")
        assert "tests/" in load_prompt("tester")

    def test_specialists_treat_publishing_as_the_handover(self) -> None:
        for name in ("planner", "writer", "tester"):
            assert "publish_work" in load_prompt(name)
            assert "refresh_from_main" in load_prompt(name)

    def test_the_writer_is_told_to_use_pydantic_and_keep_pytest_importable(self) -> None:
        text = load_prompt("writer")
        assert "Pydantic" in text and 'pythonpath = ["src"]' in text

    def test_the_tester_never_weakens_tests(self) -> None:
        assert "Never weaken a test" in load_prompt("tester")

    def test_an_empty_prompt_is_an_error(
        self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        (tmp_path / "x.md").write_text("  \n")
        monkeypatch.setattr(team_module, "PROMPTS_DIR", tmp_path)
        with pytest.raises(ValueError, match="empty"):
            load_prompt("x")


class TestParticipants:
    def test_three_specialists_with_manager_visible_descriptions(
        self, ctx: TeamContext, fake_client: FakeChatClient
    ) -> None:
        agents = build_participants(fake_client, ctx)
        assert [a.name for a in agents] == ["CodeProjectPlanner", "CodeWriter", "UnitTester"]
        assert all(a.description for a in agents)

    def test_each_specialist_gets_only_its_own_role_tools(
        self, ctx: TeamContext, fake_client: FakeChatClient
    ) -> None:
        planner, writer, tester = build_participants(fake_client, ctx)
        assert "run_tests" not in tool_names(planner)
        assert "run_tests" in tool_names(writer) and "run_tests" in tool_names(tester)
        assert all("git_commit" in tool_names(a) for a in (planner, writer, tester))

    def test_service_side_history_is_off(
        self, ctx: TeamContext, fake_client: FakeChatClient
    ) -> None:
        assert all(
            a.default_options["store"] is False for a in build_participants(fake_client, ctx)
        )

    def test_the_role_table_covers_the_file_ownership_roles(self) -> None:
        assert {role for role, _ in PARTICIPANTS.values()} == {"planner", "writer", "tester"}


class TestWorkflow:
    def test_builds_a_magentic_workflow(
        self, ctx: TeamContext, fake_client: FakeChatClient
    ) -> None:
        workflow = build_workflow(fake_client, ctx)
        assert workflow.name == "Magentic"

    def test_each_build_is_a_fresh_workflow(
        self, ctx: TeamContext, fake_client: FakeChatClient
    ) -> None:
        assert build_workflow(fake_client, ctx) is not build_workflow(fake_client, ctx)


class TestTask:
    def test_task_carries_the_process_and_the_brief(self, brief: ProjectBrief) -> None:
        task = team_task(brief)
        assert "CodeProjectPlanner" in task and "UnitTester" in task
        assert "# Project brief: slugify_lib" in task
        assert "definition of done" in task.lower()


class TestExtractFinalText:
    def test_reads_the_last_message_of_the_last_output(self) -> None:
        result = SimpleNamespace(
            get_outputs=lambda: [
                [Message(role="assistant", contents=["first"])],
                [
                    Message(role="user", contents=["x"]),
                    Message(role="assistant", contents=["final"]),
                ],
            ]
        )
        assert extract_final_text(result) == "final"

    def test_accepts_plain_strings_and_text_objects(self) -> None:
        assert extract_final_text(SimpleNamespace(get_outputs=lambda: ["plain"])) == "plain"
        assert (
            extract_final_text(SimpleNamespace(get_outputs=lambda: [SimpleNamespace(text="t")]))
            == "t"
        )

    def test_falls_back_when_there_is_nothing(self) -> None:
        assert "without a final message" in extract_final_text(SimpleNamespace(get_outputs=list))
        assert "without a final message" in extract_final_text(object())


class FakeWorkflow:
    """Stands in for the Magentic run: writes each role's files, as the real team would."""

    def __init__(self, ctx: TeamContext, *, passing: bool = True, boom: bool = False) -> None:
        self.ctx, self.passing, self.boom = ctx, passing, boom
        self.task: str | None = None

    async def run(self, task: str) -> Any:
        self.task = task
        if self.boom:
            raise RuntimeError("model unavailable")
        wt = self.ctx.manager.path_for
        files = {
            wt("planner") / "docs" / "tasks.md": "- [ ] slugify",
            wt("writer") / "pyproject.toml": '[tool.pytest.ini_options]\npythonpath = ["src"]\n',
            wt("writer")
            / "src"
            / "demo"
            / "__init__.py": "def slugify(t: str) -> str:\n    return t.lower()\n",
            wt("tester") / "tests" / "test_demo.py": (
                "from demo import slugify\n\n\ndef test_slugify():\n    assert slugify('A') == "
                + ("'a'" if self.passing else "'zzz'")
                + "\n"
            ),
        }
        for path, text in files.items():
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text)
        return SimpleNamespace(
            get_outputs=lambda: [[Message(role="assistant", contents=["All built."])]]
        )


class TestDevTeam:
    def make(
        self, ctx: TeamContext, fake_client: FakeChatClient, **flags: bool
    ) -> tuple[DevTeam, FakeWorkflow]:
        workflow = FakeWorkflow(ctx, **flags)
        return DevTeam(ctx, fake_client, workflow_factory=lambda client, c: workflow), workflow

    def test_prepare_creates_the_repo_the_worktrees_and_publishes_the_brief(
        self, ctx: TeamContext, fake_client: FakeChatClient, brief: ProjectBrief
    ) -> None:
        devteam, _ = self.make(ctx, fake_client)
        devteam.prepare(brief)
        for role in ("lead", "planner", "writer", "tester"):
            assert ctx.manager.path_for(role).is_dir()
        assert "slugify_lib" in (ctx.manager.repo / "docs" / "brief.md").read_text()
        # Every role can already see the brief.
        assert (ctx.manager.path_for("writer") / "docs" / "brief.md").exists()

    async def test_a_successful_build_is_merged_and_verified(
        self, ctx: TeamContext, fake_client: FakeChatClient, brief: ProjectBrief
    ) -> None:
        devteam, workflow = self.make(ctx, fake_client)
        report = await devteam.build_project(brief)
        assert "All built." in report
        assert "Test run on main: PASSED" in report
        for role in ("planner", "writer", "tester"):
            assert f"- {role}: merged" in report
        for relative in ("docs/tasks.md", "src/demo/__init__.py", "tests/test_demo.py"):
            assert (ctx.manager.repo / relative).exists()
        assert workflow.task is not None and "# Project brief" in workflow.task

    async def test_a_failing_test_run_is_reported_honestly(
        self, ctx: TeamContext, fake_client: FakeChatClient, brief: ProjectBrief
    ) -> None:
        devteam, _ = self.make(ctx, fake_client, passing=False)
        report = await devteam.build_project(brief)
        assert "Test run on main: FAILED" in report
        assert "PASSED" not in report

    async def test_a_crashed_team_still_leaves_a_report_and_integrates(
        self, ctx: TeamContext, fake_client: FakeChatClient, brief: ProjectBrief
    ) -> None:
        devteam, _ = self.make(ctx, fake_client, boom=True)
        report = await devteam.build_project(brief)
        assert "stopped early with an error: model unavailable" in report
        assert "Merges into main" in report
