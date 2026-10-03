"""Miscellaneous sample 9: take the user from a validated Microsoft Entra token.

Run:   ENTRA_TENANT_ID=<tenant> ENTRA_AUDIENCE=api://<app-id> \
       uvicorn hosted_agent_kit.samples.miscellaneous.entra_sign_in:app --reload

``EntraAuth.user`` checks the bearer token (signature, issuer, tenant, audience, expiry) and
returns the signed-in user's object id. ``EntraAuth.require_role`` guards administration routes
with an app role. A service that calls for its own users uses ``user_for_service``. Unlike the
other samples this one has no header sign-in, so it needs both variables and does not run offline.
"""

from __future__ import annotations

import os

from fastapi import Depends, FastAPI

from hosted_agent_kit.integrations.entra import EntraAuth
from hosted_agent_kit.integrations.fastapi import KitDep, admin_router, install
from hosted_agent_kit.samples._common import load_kit

auth = EntraAuth(
    tenant_id=os.environ.get("ENTRA_TENANT_ID", "00000000-0000-0000-0000-000000000000"),
    audience=os.environ.get("ENTRA_AUDIENCE", "api://00000000-0000-0000-0000-000000000000"),
)
kit = load_kit("agents/support-bot/scheduler.yaml")
app = FastAPI(title="Miscellaneous sample: Entra sign-in")
install(app, kit)
auth.install(app)
app.include_router(
    admin_router(dependencies=[Depends(auth.require_role("Pool.Admin"))]), prefix="/admin"
)


@app.post("/ask")
async def ask(message: str, kit: KitDep, user: str = Depends(auth.user)) -> object:
    return (await kit.ask("support-bot", message, user_id=user)).json()
