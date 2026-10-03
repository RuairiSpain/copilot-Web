"""Response sample 5: send the full Responses request and set a timeout.

Run:   uvicorn hosted_agent_kit.samples.response.request_options:app --reload

``input`` is the Responses request body, so anything the agent accepts goes through: a list of
messages, metadata, tool choices, a previous response id. ``timeout_seconds`` limits one upstream
call (it is capped by ``max_timeout_seconds``). A timeout surfaces as an error with status
504; ``retry_safe`` is true only when the agent cannot have started the work.
"""

from __future__ import annotations

from typing import Any

from fastapi import FastAPI
from pydantic import BaseModel, Field

from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.samples._common import UserDep, load_kit

kit = load_kit("agents/support-bot/scheduler.yaml")
app = FastAPI(title="Response sample: request options")
install(app, kit)


class Turn(BaseModel):
    messages: list[dict[str, str]]
    metadata: dict[str, str] = Field(default_factory=dict)
    timeout_seconds: float | None = Field(default=None, gt=0, le=300)


@app.post("/respond")
async def respond(turn: Turn, user: UserDep, kit: KitDep) -> Any:
    result = await kit.responses(
        "support-bot",
        user_id=user,
        input={"input": turn.messages, "metadata": turn.metadata},
        timeout_seconds=turn.timeout_seconds,
    )
    return result.json()
