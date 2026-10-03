"""Reporting sample 4: how much of the session quota this kit is using.

Run:   uvicorn hosted_agent_kit.samples.reporting.quota_status:app --reload
Try:   curl -s localhost:8000/quota | jq

The kit counts a session against the quota while it is leased, being created or updated, or has
been active within the idle timeout. ``kit.reporting.quota()`` shows that count, the limit now
(the static budget, lowered after a quota refusal from Foundry), and, when a ledger is shared,
the regional totals. Check the budgets of several kits before deploying with ``hack plan``.
"""

from __future__ import annotations

from fastapi import FastAPI

from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.samples._common import load_kit
from hosted_agent_kit.views import QuotaView

kit = load_kit("config/quota-budget.yaml")
app = FastAPI(title="Reporting sample: quota status")
install(app, kit)


@app.get("/quota")
async def quota(kit: KitDep) -> QuotaView:
    return await kit.reporting.quota()
