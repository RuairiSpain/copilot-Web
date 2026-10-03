"""Miscellaneous sample 7: tell Foundry which user a call is for.

Run:   POOL_USER_ISOLATION_SECRET=$(openssl rand -hex 32) \
       uvicorn hosted_agent_kit.samples.miscellaneous.user_isolation:app --reload

``user_isolation`` on an agent sends per-user headers with every call:

* ``off``: nothing per user.
* ``key``: ``x-ms-user-isolation-key``, an HMAC of the user id. Foundry keeps those users' data
  apart even when several users share one session.
* ``delegated``: the key plus ``x-ms-user-identity``, the user the call acts for.

The secret is read from ``POOL_USER_ISOLATION_SECRET`` (or ``POOL_SESSION_ID_KEY``) and is never
sent to Foundry; only the derived key is. In demo mode a fixed demo secret is used.
"""

from __future__ import annotations

from fastapi import FastAPI
from pydantic import BaseModel

from hosted_agent_kit import Hack, KitSettings
from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.samples._common import UserDep, demo_mode
from hosted_agent_kit.testing import DemoFoundry

kit = Hack.from_dict(
    {
        "agentPool": {
            "agents": {
                "support-bot": {"mode": "stateless", "user_isolation": "key"},
                "records-bot": {"mode": "stateful", "user_isolation": "delegated"},
            },
        },
    },
    settings=KitSettings(user_isolation_secret="demo-secret-not-for-production-use")  # noqa: S106  # nosec B106 - demo value
    if demo_mode()
    else None,
    adapter=DemoFoundry() if demo_mode() else None,
)
app = FastAPI(title="Miscellaneous sample: user isolation")
install(app, kit)


class Question(BaseModel):
    message: str


@app.post("/ask/{agent}")
async def ask(agent: str, question: Question, user: UserDep, kit: KitDep) -> object:
    return (await kit.ask(agent, question.message, user_id=user)).json()
