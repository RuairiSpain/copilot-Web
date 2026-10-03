"""Administrative sample 3: delete sessions.

Run:   uvicorn hosted_agent_kit.samples.administrative.session_cleanup:app --reload
Try:   curl -s -XDELETE localhost:8000/sessions/support-bot/idle -H 'X-Admin-Key: dev-admin-key'

Deleting a session that nobody is using removes it from Foundry (a finalizer holds the record
until Foundry confirms). Deleting a leased session is accepted and completes when its lease ends:
``delete_session`` returns False in that case. Deleting a user's session loses that user's
conversation state, so do it for a reason: a retention policy, a data-deletion request, a bad
version.
"""

from __future__ import annotations

from fastapi import FastAPI

from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.samples._common import AdminDep, UserDep, load_kit

kit = load_kit("agents/support-bot/scheduler.yaml")
app = FastAPI(title="Administrative sample: session cleanup")
install(app, kit)


@app.post("/seed")
async def seed(user: UserDep, kit: KitDep) -> dict[str, str]:
    await kit.ask("support-bot", "hello", user_id=user)
    return {"status": "created"}


@app.delete("/sessions/{agent}/idle", dependencies=[AdminDep])
async def delete_idle(agent: str, kit: KitDep) -> dict[str, list[str]]:
    removed: list[str] = []
    deferred: list[str] = []
    for session in await kit.reporting.sessions(agent):
        if session.local_state != "available":
            continue
        done = await kit.admin.delete_session(agent, session.session_id)
        (removed if done else deferred).append(session.session_id)
    return {"removed": removed, "deferred_until_lease_ends": deferred}
