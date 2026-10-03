"""Hosted Agent Controller Kit (HACK): a session-pool scheduler for Foundry hosted agents.

Embed it in a FastAPI application::

    from fastapi import FastAPI
    from hosted_agent_kit import Hack
    from hosted_agent_kit.integrations.fastapi import KitDep, install

    kit = Hack.from_yaml("scheduler.yaml")
    app = FastAPI()
    install(app, kit)

    @app.post("/ask")
    async def ask(text: str, user: str, kit: KitDep):
        return (await kit.ask("support-bot", text, user_id=user)).json()
"""

from hosted_agent_kit.config.models import AgentConfig, AgentPoolConfig, ConfigError
from hosted_agent_kit.config.settings import KitSettings
from hosted_agent_kit.kit import Hack
from hosted_agent_kit.results import AgentResult

__version__ = "0.2.0"

__all__ = [
    "AgentConfig",
    "AgentPoolConfig",
    "AgentResult",
    "ConfigError",
    "Hack",
    "KitSettings",
    "__version__",
]
