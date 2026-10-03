"""Administrative sample 1: expose reporting and admin endpoints behind your own guard.

Run:   uvicorn hosted_agent_kit.samples.administrative.admin_api:app --reload
Try:   curl -s localhost:8000/ops/agents -H 'X-Admin-Key: dev-admin-key' | jq

``admin_router`` can delete sessions, force a sync, add a warm session and reload the YAML.
Every endpoint changes the running pool, so the router refuses to build without ``dependencies=``.
"""

from __future__ import annotations

from fastapi import FastAPI

from hosted_agent_kit.integrations.fastapi import (
    admin_router,
    install,
    reporting_router,
)
from hosted_agent_kit.samples._common import AdminDep, load_kit

kit = load_kit("config/stateful-and-stateless.yaml")
app = FastAPI(title="Administrative sample: admin API")
install(app, kit)
app.include_router(reporting_router(dependencies=[AdminDep], reveal_identities=True), prefix="/ops")
app.include_router(admin_router(dependencies=[AdminDep]), prefix="/ops")
