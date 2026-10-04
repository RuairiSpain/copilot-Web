"""Entry point: hosts the agent through the Foundry Responses protocol on port 8088."""

from __future__ import annotations

import logging
from collections.abc import Callable

from agent_framework import Agent
from agent_framework_foundry_hosting import ResponsesHostServer
from azure.identity import DefaultAzureCredential
from dotenv import load_dotenv

from agent import Credential, build_agent
from settings import Settings

logger = logging.getLogger(__name__)


def make_agent_factory(settings: Settings, credential: Credential) -> Callable[[], Agent]:
    """Return a zero-argument factory that builds a fresh agent per request.

    Foundry guidance: create toolboxes and MCP connections inside a factory when they use
    the caller's identity, because a process-wide connection can keep the identity of the
    first request that opened it. The credential is shared and is never closed here.
    """

    def factory() -> Agent:
        return build_agent(settings, credential)

    return factory


def create_server(
    settings: Settings | None = None,
    credential: Credential | None = None,
    agent_factory: Callable[[], Agent] | None = None,
) -> ResponsesHostServer:
    """Build the Responses host. Does not start listening.

    ``agent_factory`` lets tests swap in an agent that needs no network.
    """
    settings = settings or Settings.from_env()
    credential = credential or DefaultAzureCredential()
    return ResponsesHostServer(agent_factory or make_agent_factory(settings, credential))


def main() -> None:
    load_dotenv()
    settings = Settings.from_env()
    logging.basicConfig(
        level=settings.log_level, format="%(asctime)s %(levelname)s %(name)s: %(message)s"
    )
    logger.info("Starting responses-agent (model deployment: %s)", settings.model_deployment)
    create_server(settings).run()


if __name__ == "__main__":
    main()
