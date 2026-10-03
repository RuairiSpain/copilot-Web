"""Entry point: hosts the agent through the Foundry Invocations protocol on port 8088."""

from __future__ import annotations

import logging
from collections.abc import Callable

from agent_framework import Agent
from agent_framework_foundry_hosting import InvocationsHostServer
from azure.identity import DefaultAzureCredential
from dotenv import load_dotenv

from agent import Credential, build_agent
from parsing import parse_request
from schemas import build_openapi_spec
from settings import Settings

logger = logging.getLogger(__name__)


def make_agent_factory(settings: Settings, credential: Credential) -> Callable[[], Agent]:
    """Return a factory that builds a fresh agent per request.

    The toolbox connection uses the caller's identity, so it is created per request. The
    credential is shared and never closed here.
    """

    def factory() -> Agent:
        return build_agent(settings, credential)

    return factory


def create_server(
    settings: Settings | None = None,
    credential: Credential | None = None,
    agent_factory: Callable[[], Agent] | None = None,
) -> InvocationsHostServer:
    """Build the Invocations host. Does not start listening.

    ``agent_factory`` lets tests swap in an agent that needs no network.
    """
    settings = settings or Settings.from_env()
    credential = credential or DefaultAzureCredential()
    return InvocationsHostServer(
        agent_factory or make_agent_factory(settings, credential),
        parse_request=parse_request,
        openapi_spec=build_openapi_spec(),
        # Fail loudly if a caller sends options this agent cannot honour.
        unsupported_options="error",
    )


def main() -> None:
    load_dotenv()
    settings = Settings.from_env()
    logging.basicConfig(
        level=settings.log_level, format="%(asctime)s %(levelname)s %(name)s: %(message)s"
    )
    logger.info("Starting invocations-agent (model deployment: %s)", settings.model_deployment)
    create_server(settings).run()


if __name__ == "__main__":
    main()
