"""Response sample 6: read a stream as typed events and text, instead of raw bytes.

Run:   uvicorn hosted_agent_kit.samples.response.stream_events:app --reload
Try:   curl -s localhost:8000/chat -H 'X-User-Id: alice' \
            -H 'content-type: application/json' -d '{"message": "refund policy?"}'

``result.text_deltas()`` yields the answer text piece by piece. ``result.events()`` yields every
event (``SseEvent``: ``event``, ``data``, ``json()``). A stream that ends with an error event
raises ``StreamError``, so a failed answer is never returned as if it were complete.
"""

from __future__ import annotations

from fastapi import FastAPI
from pydantic import BaseModel

from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.results import StreamError
from hosted_agent_kit.samples._common import UserDep, load_kit

kit = load_kit("agents/support-bot/scheduler.yaml")
app = FastAPI(title="Response sample: stream events")
install(app, kit)


class Question(BaseModel):
    message: str


@app.post("/chat")
async def chat(question: Question, user: UserDep, kit: KitDep) -> dict[str, object]:
    pieces: list[str] = []
    event_names: list[str] = []
    try:
        async with await kit.responses(
            "support-bot", user_id=user, input={"input": question.message}, stream=True
        ) as result:
            async for event in result.events():
                event_names.append(event.event)
                if event.event.endswith(".delta"):
                    pieces.append(str(event.json().get("delta", "")))
    except StreamError as error:
        return {"complete": False, "partial": "".join(pieces), "error": str(error)}
    return {"complete": True, "text": "".join(pieces), "events": event_names}
