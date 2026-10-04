"""The Magentic team: a planner, a code writer and a unit tester, coordinated by a manager.

Roles and what they own:

=================  ======================  ==================================
Participant        Role key (files/tools)  Produces
=================  ======================  ==================================
CodeProjectPlanner planner                 docs/requirements.md, docs/tasks.md
CodeWriter         writer                  src/ (Pydantic models), pyproject.toml
UnitTester         tester                  tests/ and the passing test run
=================  ======================  ==================================

The Magentic manager only plans and routes. It has no tools, by design of the framework,
so the lead agent (see ``lead.py``) is the agent that owns the user conversation.
"""

from __future__ import annotations

import logging
from collections.abc import Callable
from pathlib import Path
from typing import Any

from agent_framework import Agent
from agent_framework.orchestrations import MagenticBuilder

from intake.brief import ProjectBrief
from tools import files, shell
from tools.agent_tools import TeamContext, build_role_tools

logger = logging.getLogger(__name__)

PROMPTS_DIR = Path(__file__).parent / "prompts"
# Participant name -> (role key, description the manager reads when choosing who acts next).
PARTICIPANTS: dict[str, tuple[str, str]] = {
    "CodeProjectPlanner": (
        "planner",
        "Turns the approved brief into docs/requirements.md and an ordered docs/tasks.md "
        "for the code writer. Writes documentation and guides. Never writes code.",
    ),
    "CodeWriter": (
        "writer",
        "Implements the tasks as typed Pydantic Python under src/ and keeps pyproject.toml "
        "correct. Never writes tests.",
    ),
    "UnitTester": (
        "tester",
        "Writes unit tests under tests/ for every requirement and sample, runs them in the "
        "agent host, and reports failures precisely. Never edits src/.",
    ),
}
ROLE_ORDER = ("planner", "writer", "tester")


def load_prompt(name: str) -> str:
    """Read ``prompts/<name>.md``. Prompts live in files so changes are easy to review."""
    path = PROMPTS_DIR / f"{name}.md"
    text = path.read_text(encoding="utf-8").strip()
    if not text:
        raise ValueError(f"Prompt file is empty: {path}")
    return text


def build_participants(client: Any, ctx: TeamContext) -> list[Agent]:
    """One agent per participant, each with only its own role's tools."""
    agents = []
    for name, (role, description) in PARTICIPANTS.items():
        agents.append(
            Agent(
                client=client,
                name=name,
                description=description,
                instructions=load_prompt(role),
                tools=build_role_tools(role, ctx),
                default_options={"store": False},
            )
        )
    return agents


def build_workflow(client: Any, ctx: TeamContext) -> Any:
    """A fresh Magentic workflow. Build one per run: runs are not isolated from each other."""
    settings = ctx.settings
    manager = Agent(
        client=client,
        name="MagenticManager",
        description="Plans the work and decides which specialist acts next.",
        instructions=load_prompt("manager"),
        default_options={"store": False},
    )
    return MagenticBuilder(
        participants=build_participants(client, ctx),
        manager_agent=manager,
        max_round_count=settings.max_round_count,
        max_stall_count=settings.max_stall_count,
        max_reset_count=settings.max_reset_count,
    ).build()


def team_task(brief: ProjectBrief) -> str:
    """The task text the Magentic manager receives."""
    return (
        "Build the project in the approved brief below.\n\n"
        "Process: CodeProjectPlanner writes docs/requirements.md and docs/tasks.md and "
        "publishes. CodeWriter refreshes, implements the tasks in src/ and publishes. "
        "UnitTester refreshes, writes tests for every requirement and sample, runs them, "
        "and reports. If tests fail, the failure goes back to CodeWriter. Finish only when "
        "every item in the definition of done is met.\n\n"
        f"{brief.to_markdown()}"
    )


def extract_final_text(result: Any) -> str:
    """Pull the final answer out of a workflow run result, whatever shape it takes."""
    outputs = list(result.get_outputs()) if hasattr(result, "get_outputs") else []
    for item in reversed(outputs):
        if isinstance(item, list) and item:
            last = item[-1]
            text = getattr(last, "text", None)
            if text:
                return str(text)
        text = getattr(item, "text", None)
        if text:
            return str(text)
        if isinstance(item, str) and item.strip():
            return item
    return "The team finished without a final message."


class DevTeam:
    """Runs one build: sets up the repository, runs the team, merges and verifies."""

    def __init__(
        self,
        ctx: TeamContext,
        client: Any,
        *,
        workflow_factory: Callable[[Any, TeamContext], Any] = build_workflow,
    ) -> None:
        self.ctx = ctx
        self.client = client
        self._workflow_factory = workflow_factory

    def prepare(self, brief: ProjectBrief) -> None:
        """Create the repository and one worktree per role, and publish the brief."""
        manager = self.ctx.manager
        manager.ensure_repo()
        for role in files.ROLES:
            manager.add(role)
        files.write_file(manager.path_for("lead"), "lead", "docs/brief.md", brief.to_markdown())
        manager.publish("lead", "Add the approved project brief")
        for role in ROLE_ORDER:
            manager.refresh(role)

    def integrate(self) -> list[str]:
        """Publish every role's last work to main. Returns one line per role."""
        lines = []
        for role in ROLE_ORDER:
            result = self.ctx.manager.publish(role, f"Final {role} changes")
            status = "merged" if result.ok else f"CONFLICT in {', '.join(result.conflicts)}"
            lines.append(f"- {role}: {status}")
        return lines

    def verify(self) -> shell.CommandResult:
        """Run the full test suite on main, in the agent host."""
        settings = self.ctx.settings
        return shell.run_command(
            "python -m pytest -q",
            cwd=self.ctx.manager.repo,
            root=self.ctx.manager.repo,
            timeout=settings.command_timeout_seconds,
            max_output_chars=settings.max_output_chars,
        )

    async def build_project(self, brief: ProjectBrief) -> str:
        """Run the whole build and return a report for the lead agent to relay."""
        self.prepare(brief)
        workflow = self._workflow_factory(self.client, self.ctx)
        try:
            result = await workflow.run(team_task(brief))
            final = extract_final_text(result)
        except Exception as exc:  # the report must still cover what was built
            logger.exception("The Magentic run failed")
            final = f"The team stopped early with an error: {exc}"
        merges = self.integrate()
        tests = self.verify()
        verdict = "PASSED" if tests.ok else "FAILED"
        return (
            f"Team report:\n{final}\n\n"
            f"Merges into main:\n" + "\n".join(merges) + "\n\n"
            f"Test run on main: {verdict}\n{tests.render()}\n\n"
            f"History:\n{self.ctx.manager.log()}"
        )
