"""Administrative sample 2: add capacity, force a sync, reload configuration.

Run:   uvicorn hosted_agent_kit.samples.administrative.capacity_control:app --reload
Try:   curl -s -XPOST localhost:8000/capacity/support-bot/warm -H 'X-Admin-Key: dev-admin-key'

- ``provision_warm`` creates one ready session before traffic needs it. It returns False when
  the pool is full.
- ``sync`` compares Foundry's sessions with the pool's view now, instead of at the next interval.
  The report lists what it found and removed.
- ``reload_config`` re-reads the YAML. Changing sizes, queues, scheduler profiles or the pinned
  version applies at once; adding or removing agents needs a restart.
"""

from __future__ import annotations

from fastapi import FastAPI

from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.samples._common import AdminDep, load_kit
from hosted_agent_kit.views import ReloadResult, SyncReportView

kit = load_kit("agents/support-bot/scheduler.yaml")
app = FastAPI(title="Administrative sample: capacity")
install(app, kit)


@app.post("/capacity/{agent}/warm", dependencies=[AdminDep])
async def warm(agent: str, kit: KitDep) -> dict[str, bool]:
    return {"created": await kit.admin.provision_warm(agent)}


@app.post("/capacity/{agent}/sync", response_model=SyncReportView, dependencies=[AdminDep])
async def sync(agent: str, kit: KitDep) -> SyncReportView:
    return await kit.admin.sync(agent)


@app.post("/config/reload", response_model=ReloadResult, dependencies=[AdminDep])
async def reload(kit: KitDep) -> ReloadResult:
    return kit.admin.reload_config()
