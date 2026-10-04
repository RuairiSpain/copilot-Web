"""Entry point: hosts the lead agent through the Foundry Responses protocol on port 8088."""

from __future__ import annotations

import logging
from collections.abc import Callable
from typing import Any

from agent_framework import Agent
from agent_framework.foundry import FoundryChatClient
from agent_framework_foundry_hosting import ResponsesHostServer
from azure.core.credentials import TokenCredential
from azure.core.credentials_async import AsyncTokenCredential
from azure.identity import DefaultAzureCredential
from dotenv import load_dotenv

from lead import build_lead_agent
from settings import Settings
from tools.agent_tools import TeamContext
from tools.worktrees import WorktreeManager

logger = logging.getLogger(__name__)
Credential = TokenCredential | AsyncTokenCredential
PROJECT_FOLDER = "project"


def build_context(settings: Settings) -> TeamContext:
    """The shared project: one git repository under the workspace root."""
    repo = settings.workspace_root / PROJECT_FOLDER
    return TeamContext(settings, WorktreeManager(repo))


def make_agent_factory(
    settings: Settings, credential: Credential, client: Any | None = None
) -> Callable[[], Agent]:
    """Return a factory that builds a fresh lead agent per request.

    State lives on disk (the git repository), not in the agent, so a new agent per
    request loses nothing. The credential is shared and never closed here.
    """

    def factory() -> Agent:
        chat = client or FoundryChatClient(
            project_endpoint=settings.project_endpoint,
            model=settings.model_deployment,
            credential=credential,
        )
        return build_lead_agent(build_context(settings), chat)

    return factory


def create_server(
    settings: Settings | None = None,
    credential: Credential | None = None,
    agent_factory: Callable[[], Agent] | None = None,
) -> ResponsesHostServer:
    """Build the Responses host. Does not start listening."""
    settings = settings or Settings.from_env()
    credential = credential or DefaultAzureCredential()
    return ResponsesHostServer(agent_factory or make_agent_factory(settings, credential))


def main() -> None:
    load_dotenv()
    settings = Settings.from_env()
    logging.basicConfig(
        level=settings.log_level, format="%(asctime)s %(levelname)s %(name)s: %(message)s"
    )
    logger.info("Starting dev-team (workspace: %s)", settings.workspace_root)
    create_server(settings).run()


if __name__ == "__main__":
    main()
