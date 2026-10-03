"""Response sample 2: stream an answer to your caller as server-sent events.

Run:   uvicorn hosted_agent_kit.samples.response.streaming:app --reload
Try:   curl -N localhost:8000/chat/stream -H 'X-User-Id: alice' \
            -H 'content-type: application/json' -d '{"message": "refund policy?"}'

With ``stream=True`` the result is a stream. ``sse_response`` relays it and returns the session to
the pool when the stream ends, even if your caller disconnects. A failure after streaming has
started arrives as a final ``event: error`` frame, because the HTTP status is already sent.
"""

from __future__ import annotations

from fastapi import FastAPI, Response
from pydantic import BaseModel

from hosted_agent_kit.integrations.fastapi import KitDep, install, sse_response
from hosted_agent_kit.samples._common import UserDep, load_kit

kit = load_kit("agents/support-bot/scheduler.yaml")
app = FastAPI(title="Response sample: streaming")
install(app, kit)


class Question(BaseModel):
    message: str


@app.post("/chat/stream")
async def chat_stream(question: Question, user: UserDep, kit: KitDep) -> Response:
    result = await kit.responses(
        "support-bot", user_id=user, input={"input": question.message}, stream=True
    )
    return sse_response(result)


@app.post("/chat/collect")
async def chat_collect(question: Question, user: UserDep, kit: KitDep) -> dict[str, str]:
    """Consume the stream inside your endpoint instead of relaying it."""
    text = []
    async with await kit.responses(
        "support-bot", user_id=user, input={"input": question.message}, stream=True
    ) as result:
        async for frame in result.chunks():
            text.append(frame.decode())
    return {"raw_events": "".join(text)}
