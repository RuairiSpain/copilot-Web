"""support-bot: a stateless Responses-protocol hosted agent.

The Foundry hosted-agent contract is small: listen on port 8088, answer ``GET /readiness``, and
serve the protocol the agent declares. This file implements the Responses protocol with plain
FastAPI so you can see the whole contract. Replace ``answer`` with your model or tool calls.
"""

from __future__ import annotations

import json
from collections.abc import AsyncIterator
from typing import Any

from fastapi import FastAPI
from fastapi.responses import JSONResponse, StreamingResponse

app = FastAPI(title="support-bot")

KNOWLEDGE = {
    "refund": "Refunds are issued to the original payment method within 5 business days.",
    "shipping": "Standard shipping takes 3 to 5 business days.",
}


def answer(text: str) -> str:
    for topic, reply in KNOWLEDGE.items():
        if topic in text.lower():
            return reply
    return "I can help with refunds and shipping. What do you need?"


def _text_of(body: dict[str, Any]) -> str:
    value = body.get("input", "")
    if isinstance(value, list):  # Responses allows a list of messages
        return " ".join(str(m.get("content", "")) for m in value if isinstance(m, dict))
    return str(value)


@app.get("/readiness")
async def readiness() -> dict[str, str]:
    return {"status": "ready"}


@app.post("/responses")
async def responses(body: dict[str, Any]) -> Any:
    reply = answer(_text_of(body))
    if body.get("stream"):
        return StreamingResponse(_stream(reply), media_type="text/event-stream")
    return JSONResponse(
        {
            "object": "response",
            "status": "completed",
            "output": [
                {
                    "type": "message",
                    "role": "assistant",
                    "content": [{"type": "output_text", "text": reply}],
                }
            ],
            "output_text": reply,
        }
    )


async def _stream(reply: str) -> AsyncIterator[bytes]:
    for word in reply.split():
        data = json.dumps({"delta": word + " "})
        yield f"event: response.output_text.delta\ndata: {data}\n\n".encode()
    yield b'event: response.completed\ndata: {"status": "completed"}\n\n'
