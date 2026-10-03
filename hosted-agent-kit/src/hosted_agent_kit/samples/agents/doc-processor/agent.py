"""doc-processor: an Invocations-protocol hosted agent.

The Invocations protocol has no fixed request or response format. This agent accepts plain text
or JSON and answers with a JSON summary. It could as well accept a PDF and return a PNG: the
scheduler relays bytes and content types unchanged.
"""

from __future__ import annotations

import json

from fastapi import FastAPI, Request, Response

app = FastAPI(title="doc-processor")


@app.get("/readiness")
async def readiness() -> dict[str, str]:
    return {"status": "ready"}


@app.post("/invocations")
async def invocations(request: Request) -> Response:
    body = await request.body()
    content_type = request.headers.get("content-type", "application/octet-stream")
    if content_type.startswith("application/json"):
        text = json.dumps(json.loads(body or b"{}"))
    else:
        text = body.decode("utf-8", errors="replace")
    summary = {
        "content_type": content_type,
        "bytes": len(body),
        "words": len(text.split()),
        "preview": text[:80],
    }
    return Response(json.dumps(summary), media_type="application/json")
