"""Administrative actions: delete sessions, force a sync, reload configuration, add capacity."""

from __future__ import annotations

from hosted_agent_kit.domain.errors import ReloadUnavailableError
from hosted_agent_kit.runtime import Runtime
from hosted_agent_kit.views import ReloadResult, SyncReportView


class Administration:
    def __init__(self, runtime: Runtime) -> None:
        self._rt = runtime

    async def delete_session(self, agent_name: str, session_id: str) -> bool:
        """Delete a session. False means it is leased and is deleted when the lease ends."""
        return await self._rt.pool.admin_delete(agent_name, session_id)

    async def sync(self, agent_name: str) -> SyncReportView:
        """Compare Foundry with the local view now instead of waiting for the next resync."""
        self._rt.pool.agent_config(agent_name)
        report = await self._rt.reconciler.run_agent(agent_name, manual=True)
        return SyncReportView(
            agent_name=report.agent_name,
            outcome=report.outcome,
            discovered=report.discovered,
            removed=report.removed,
            failed_sessions=report.failed_sessions,
            uncertain=report.uncertain,
            warm_created=report.warm_created,
            drained=report.drained,
            duration_seconds=round(report.duration_seconds, 6),
            errors=list(report.errors),
        )

    def reload_config(self) -> ReloadResult:
        """Re-read the scheduler configuration. Adding or removing agents needs a restart."""
        if self._rt.reloader is None:
            raise ReloadUnavailableError("This instance was not created from a configuration file.")
        return ReloadResult(changed=self._rt.reloader.reload())

    async def provision_warm(self, agent_name: str) -> bool:
        """Create one unbound ready session if the pool has capacity. False when it is full."""
        return await self._rt.pool.provision_warm(agent_name)
