"""Invoke sample 3: one endpoint that relays any request to any Invocations agent.

Run:   uvicorn hosted_agent_kit.samples.invoke.passthrough:app --reload
Try:   curl -s localhost:8000/raw/doc-processor -H 'X-User-Id: alice' \
            -H 'content-type: application/json' -d '{"any": "shape"}'

Useful for a gateway in front of several agents. ``kit.describe`` tells you each agent's protocol
so the endpoint can refuse a Responses agent with a clear message, and ``X-Conversation-Key``
selects a conversation when the agent is stateful.
"""

from __future__ import annotations

from typing import Annotated

from fastapi import FastAPI, Header, Request, Response

from hosted_agent_kit.integrations.fastapi import KitDep, install, response_for
from hosted_agent_kit.samples._common import UserDep, load_kit

kit = load_kit("agents/doc-processor/scheduler.yaml")
app = FastAPI(title="Invoke sample: passthrough")
install(app, kit)


@app.post("/raw/{agent}")
async def relay(
    agent: str,
    request: Request,
    user: UserDep,
    kit: KitDep,
    x_conversation_key: Annotated[str | None, Header()] = None,
) -> Response:
    info = kit.describe(agent)  # unknown agent: 404 problem+json
    if info.protocol != "invocations":
        # Responses agents have a fixed shape; use kit.responses() for them.
        return Response(f"{agent} is a {info.protocol} agent", status_code=422)
    result = await kit.invocations(
        agent,
        user_id=user,
        body=await request.body(),
        content_type=request.headers.get("content-type"),
        conversation_key=x_conversation_key if info.stateful else None,
    )
    return response_for(result)
