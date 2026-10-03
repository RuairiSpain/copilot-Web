"""Miscellaneous sample 8: one agent served by several kits, each owning a shard.

Run:   uvicorn hosted_agent_kit.samples.miscellaneous.sharded_gateway:app --reload
Try:   curl -s localhost:8000/route -H 'X-User-Id: alice'

Each kit is configured with ``shard: {index, count}`` and owns the sessions whose ids carry its
shard prefix. A user always maps to the same shard (``kit.shard_for``). A kit that receives a call
for another shard's user raises ``WrongShardError`` (HTTP 421), so a load balancer or gateway
can send the call to the right kit. This sample shows the routing decision for kit 0 of 2; see
``config/ownership-sharding.yaml`` for the settings.
"""

from __future__ import annotations

from fastapi import FastAPI

from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.samples._common import UserDep, load_kit

kit = load_kit("config/ownership-sharding.yaml")
app = FastAPI(title="Miscellaneous sample: sharded gateway")
install(app, kit)


@app.get("/route")
async def route(user: UserDep, kit: KitDep) -> dict[str, object]:
    shard = kit.shard_for(user)
    return {"user": user, "shard": shard, "role": kit.role, "ready": kit.ready}


@app.post("/ask")
async def ask(message: str, user: UserDep, kit: KitDep) -> object:
    """Answers when the user belongs to this shard; otherwise the kit answers 421."""
    return (await kit.ask("support-bot", message, user_id=user)).json()
