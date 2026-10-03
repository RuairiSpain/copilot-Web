"""Reporting sample 3: inspect sessions, their conditions and their owners.

Run:   uvicorn hosted_agent_kit.samples.reporting.session_inspector:app --reload
Try:   curl -s -XPOST localhost:8000/seed -H 'X-User-Id: alice'     curl -s localhost:8000/sessions

Each session has a platform status, a local state (available, leased, retiring, unavailable),
the user and conversation it is bound to, and Kubernetes-style fields: ``generation``,
``resource_version``, ``finalizers`` and ``conditions``. User ids and conversation keys are hashed
unless you pass ``reveal_identities=True``. Reveal them only to staff who may see them.
"""

from __future__ import annotations

from fastapi import FastAPI

from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.samples._common import UserDep, load_kit
from hosted_agent_kit.views import SessionAdminView

kit = load_kit("agents/memory-bot/scheduler.yaml")
app = FastAPI(title="Reporting sample: session inspector")
install(app, kit)


@app.post("/seed")
async def seed(user: UserDep, kit: KitDep) -> dict[str, str]:
    """Create a session for this user so there is something to inspect."""
    await kit.ask("memory-bot", "remember hello", user_id=user)
    return {"status": "created"}


@app.get("/sessions", response_model=list[SessionAdminView])
async def sessions(kit: KitDep) -> list[SessionAdminView]:
    return await kit.reporting.sessions("memory-bot")


@app.get("/sessions/revealed", response_model=list[SessionAdminView])
async def sessions_revealed(kit: KitDep) -> list[SessionAdminView]:
    """Raw user ids and conversation keys. Put real authorisation on an endpoint like this."""
    return await kit.reporting.sessions("memory-bot", reveal_identities=True)
