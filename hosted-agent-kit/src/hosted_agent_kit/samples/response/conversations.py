"""Response sample 3: a stateful agent with several conversations per user.

Run:   uvicorn hosted_agent_kit.samples.response.conversations:app --reload
Try:   curl -s localhost:8000/conversations/trip/messages -H 'X-User-Id: alice' \
            -H 'content-type: application/json' -d '{"message": "remember I like trains"}'

A stateful agent keeps data in its session. The pool sends one user back to the same session, and
``conversation_key`` gives that user a separate session for each conversation. Keys belong to the
user: two users using the key ``trip`` never share a session.
"""

from __future__ import annotations

from fastapi import FastAPI
from pydantic import BaseModel

from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.samples._common import UserDep, load_kit

kit = load_kit("agents/memory-bot/scheduler.yaml")
app = FastAPI(title="Response sample: conversations")
install(app, kit)


class Message(BaseModel):
    message: str


@app.post("/conversations/{conversation}/messages")
async def send(conversation: str, body: Message, user: UserDep, kit: KitDep) -> dict[str, object]:
    result = await kit.ask("memory-bot", body.message, user_id=user, conversation_key=conversation)
    return {"conversation": conversation, "reply": result.json()}


@app.post("/messages")
async def send_default(body: Message, user: UserDep, kit: KitDep) -> dict[str, object]:
    """Without a conversation key, the user has one session for the agent."""
    result = await kit.ask("memory-bot", body.message, user_id=user)
    return {"reply": result.json()}
