"""Response sample 4: make a retried request safe with an idempotency key.

Run:   uvicorn hosted_agent_kit.samples.response.idempotent_requests:app --reload
Try:   curl -s -XPOST localhost:8000/orders/summary -H 'X-User-Id: alice' \
            -H 'Idempotency-Key: order-42' -H 'content-type: application/json' \
            -d '{"message": "summarise order 42"}'      # run it twice

The first call runs the agent. A repeat with the same key and the same request returns the stored
answer (``replayed`` is true) without calling the agent again. The same key with a different
request is refused with 422 (IDEMPOTENCY_KEY_REUSED), and a repeat while the first call is
still running with 409 (IDEMPOTENCY_IN_PROGRESS).
Only successful, non-streaming answers are stored, and only in this process's memory.
"""

from __future__ import annotations

from typing import Annotated

from fastapi import FastAPI, Header
from pydantic import BaseModel

from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.samples._common import UserDep, load_kit

kit = load_kit("agents/support-bot/scheduler.yaml")
app = FastAPI(title="Response sample: idempotency")
install(app, kit)


class Request(BaseModel):
    message: str


@app.post("/orders/summary")
async def summary(
    body: Request,
    user: UserDep,
    kit: KitDep,
    idempotency_key: Annotated[str | None, Header()] = None,
) -> dict[str, object]:
    result = await kit.ask(
        "support-bot", body.message, user_id=user, idempotency_key=idempotency_key
    )
    return {"replayed": result.replayed, "answer": result.json()}
