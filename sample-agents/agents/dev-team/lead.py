"""The lead agent: interviews the user, then hands a validated brief to the team.

The lead is the "main" agent. It owns the conversation, the intake, and the final
report. The Magentic team does the building. The lead gets the same CLI and git tools as
the other roles, scoped to its own worktree.
"""

from __future__ import annotations

import json
from collections.abc import Awaitable, Callable
from typing import Annotated, Any

from agent_framework import Agent, FunctionTool, tool

from intake.brief import ProjectBrief
from intake.gate import evaluate_brief
from team import DevTeam, load_prompt
from tools.agent_tools import TeamContext, build_role_tools

AGENT_NAME = "dev-team-lead"
AGENT_DESCRIPTION = "Interviews you, then plans, writes and tests a Pydantic Python project."

Runner = Callable[[ProjectBrief], Awaitable[str]]


def build_instructions() -> str:
    """The lead's prompt plus the live brief schema, so the two can never drift apart."""
    schema = json.dumps(ProjectBrief.model_json_schema(), indent=2)
    return f"{load_prompt('lead')}\n\n## Brief JSON schema\n\n```json\n{schema}\n```"


def build_lead_tools(ctx: TeamContext, runner: Runner) -> list[FunctionTool]:
    """Role tools for the lead, plus the two tools that guard and start a build."""

    @tool(
        name="validate_brief",
        description=(
            "Check a draft project brief. Returns the questions still to ask the user, or "
            "confirms the brief is complete. Call it every time the draft changes."
        ),
    )
    def validate_brief(brief_json: Annotated[str, "The brief as a JSON object"]) -> str:
        return evaluate_brief(brief_json).render()

    @tool(
        name="start_build",
        description=(
            "Start the build. Only call this after the user has explicitly confirmed the "
            "brief. The brief is validated again and the build is refused if it is incomplete."
        ),
    )
    async def start_build(
        brief_json: Annotated[str, "The confirmed brief as a JSON object"],
    ) -> str:
        result = evaluate_brief(brief_json)
        if not result.ok or result.brief is None:
            return result.render()
        return await runner(result.brief)

    return [*build_role_tools("lead", ctx), validate_brief, start_build]


def build_lead_agent(
    ctx: TeamContext,
    client: Any,
    *,
    runner: Runner | None = None,
) -> Agent:
    """Create the lead agent. ``runner`` defaults to a real :class:`DevTeam` build."""
    team_runner = runner or DevTeam(ctx, client).build_project
    return Agent(
        client=client,
        name=AGENT_NAME,
        description=AGENT_DESCRIPTION,
        instructions=build_instructions(),
        tools=build_lead_tools(ctx, team_runner),
        default_options={"store": False},
    )
