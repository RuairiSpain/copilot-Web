"""Miscellaneous sample 5: one endpoint for several agents of both protocols.

Run:   uvicorn hosted_agent_kit.samples.miscellaneous.multi_agent_gateway:app --reload
Try:   curl -s localhost:8000/agents | jq
       curl -s localhost:8000/call/doc-processor -H 'X-User-Id: alice' -d 'plain text'

``kit.describe`` returns an agent's protocol and capabilities, so a gateway can pick
``responses`` or ``invocations`` without hard-coding which agent uses which.
"""

from __future__ import annotations

from fastapi import FastAPI, Request, Response

from hosted_agent_kit.integrations.fastapi import KitDep, install, response_for
from hosted_agent_kit.samples._common import UserDep, load_kit

kit = load_kit("config/all-protocols.yaml")
app = FastAPI(title="Miscellaneous sample: multi-agent gateway")
install(app, kit)


@app.get("/agents")
async def agents(kit: KitDep) -> list[dict[str, object]]:
    return [info.model_dump() for info in kit.list_agents()]


@app.post("/call/{agent}")
async def call(agent: str, request: Request, user: UserDep, kit: KitDep) -> Response:
    info = kit.describe(agent)
    body = await request.body()
    if info.protocol == "responses":
        result = await kit.responses(agent, user_id=user, input={"input": body.decode()})
    else:
        result = await kit.invocations(
            agent, user_id=user, body=body, content_type=request.headers.get("content-type")
        )
    return response_for(result)
