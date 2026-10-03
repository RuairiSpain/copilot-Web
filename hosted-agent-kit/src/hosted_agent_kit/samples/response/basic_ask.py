"""Response sample 1: ask a Responses agent one question.

Run:   uvicorn hosted_agent_kit.samples.response.basic_ask:app --reload
Try:   curl -s localhost:8000/ask -H 'X-User-Id: alice' -H 'content-type: application/json' \
            -d '{"message": "How long does shipping take?"}'

``kit.ask`` picks a free session from the pool (creating one if there is capacity), sends the
message, and returns the session to the pool. Your endpoint never handles session ids.
"""

from __future__ import annotations

from fastapi import FastAPI
from pydantic import BaseModel

from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.samples._common import UserDep, load_kit

kit = load_kit("agents/support-bot/scheduler.yaml")
app = FastAPI(title="Response sample: basic ask")
install(app, kit)  # starts the kit with the app, and maps kit errors to HTTP responses


class Question(BaseModel):
    message: str


@app.post("/ask")
async def ask(question: Question, user: UserDep, kit: KitDep) -> dict[str, object]:
    result = await kit.ask("support-bot", question.message, user_id=user)
    return {"answer": result.json()}


@app.get("/agents")
async def agents(kit: KitDep) -> list[dict[str, object]]:
    """What a caller may do with each configured agent."""
    return [info.model_dump() for info in kit.list_agents()]
