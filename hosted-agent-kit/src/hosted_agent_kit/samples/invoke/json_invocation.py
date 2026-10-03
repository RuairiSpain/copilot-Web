"""Invoke sample 1: call an Invocations agent with a JSON body.

Run:   uvicorn hosted_agent_kit.samples.invoke.json_invocation:app --reload
Try:   curl -s localhost:8000/summarise -H 'X-User-Id: alice' \
            -H 'content-type: application/json' -d '{"title": "Q3", "body": "revenue grew"}'

The Invocations protocol has no fixed format: the agent defines its own request and response.
Pass a mapping and the kit sends it as JSON. ``result.status_code``, ``result.media_type`` and
``result.content`` are exactly what the agent returned.
"""

from __future__ import annotations

from typing import Any

from fastapi import FastAPI

from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.samples._common import UserDep, load_kit

kit = load_kit("agents/doc-processor/scheduler.yaml")
app = FastAPI(title="Invoke sample: JSON")
install(app, kit)


@app.post("/summarise")
async def summarise(document: dict[str, Any], user: UserDep, kit: KitDep) -> Any:
    result = await kit.invocations("doc-processor", user_id=user, body=document)
    return result.json()
