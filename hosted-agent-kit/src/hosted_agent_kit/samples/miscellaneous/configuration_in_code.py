"""Miscellaneous sample 4: configure the kit without a YAML file.

Run:   uvicorn hosted_agent_kit.samples.miscellaneous.configuration_in_code:app --reload

Three ways to configure, from most to least code:

1. ``Hack.from_dict({...})``: the same structure as the YAML, built in Python.
2. ``Hack.from_yaml(path)``: a YAML file, or the ``agentPool`` section of ``azure.yaml``.
3. ``Hack.from_yaml(path)`` plus ``POOL_*`` environment variables, which override the optional
   ``hack:`` section of the file (timeouts, limits, retries).

``KitSettings`` carries the runtime knobs. Secrets and the Foundry endpoint come from the
environment (``FOUNDRY_PROJECT_ENDPOINT``, ``POOL_SESSION_ID_KEY``), never from the file.
"""

from __future__ import annotations

from typing import Any

from fastapi import FastAPI

from hosted_agent_kit import Hack, KitSettings
from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.samples._common import UserDep, demo_mode
from hosted_agent_kit.testing import DemoFoundry

kit = Hack.from_dict(
    {
        "agentPool": {
            "defaults": {"mode": "stateless", "max_sessions": 4},
            "agents": {
                "support-bot": {"min_warm_sessions": 1, "queue": {"max_wait_seconds": 20}},
            },
        },
    },
    settings=KitSettings(default_timeout_seconds=60, max_timeout_seconds=300),
    adapter=DemoFoundry() if demo_mode() else None,
)
app = FastAPI(title="Miscellaneous sample: configuration in code")
install(app, kit)


@app.post("/ask")
async def ask(message: str, user: UserDep, kit: KitDep) -> Any:
    return (await kit.ask("support-bot", message, user_id=user)).json()
