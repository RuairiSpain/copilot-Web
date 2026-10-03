"""Invoke sample 2: send a file to an Invocations agent and relay its answer.

Run:   uvicorn hosted_agent_kit.samples.invoke.file_upload:app --reload
Try:   curl -s localhost:8000/process -H 'X-User-Id: alice' \
            -H 'content-type: text/plain' --data-binary @README.md

The body can be any media type: text, a PDF, an image, a zip. Send the bytes with their content
type. ``response_for`` returns the agent's answer with its own status code and content type, so a
JSON summary, a PNG or a CSV all work.
"""

from __future__ import annotations

from fastapi import FastAPI, Request, Response

from hosted_agent_kit.integrations.fastapi import KitDep, install, response_for
from hosted_agent_kit.samples._common import UserDep, load_kit

kit = load_kit("agents/doc-processor/scheduler.yaml")
app = FastAPI(title="Invoke sample: file upload")
install(app, kit)


@app.post("/process")
async def process(request: Request, user: UserDep, kit: KitDep) -> Response:
    result = await kit.invocations(
        "doc-processor",
        user_id=user,
        body=await request.body(),
        content_type=request.headers.get("content-type"),
        timeout_seconds=120,
    )
    return response_for(result)
