"""Port for the shared regional view: spare-pool permits and each kit's published count."""

from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass, field
from typing import Protocol


@dataclass(frozen=True)
class RegionalSnapshot:
    """What every kit that shares the ledger reports, for one subscription and region."""

    region_limit: int | None
    total_active: int  # sum of the counts the live kits published
    spare_size: int
    spare_used: int
    kits: dict[str, int] = field(default_factory=dict)  # kit id -> active sessions it reported

    @property
    def headroom(self) -> int | None:
        if self.region_limit is None:
            return None
        return max(0, self.region_limit - self.total_active)


class QuotaLedger(Protocol):
    """Shared state for borrowing and visibility. It is derived state: Foundry is the truth.

    Entries expire, so a kit that crashes stops counting without cleanup. Losing the ledger loses
    nothing that cannot be rebuilt from the kits' next heartbeat.
    """

    async def start(self) -> None: ...

    async def close(self) -> None: ...

    async def acquire_spare(self, kit_id: str, token: str, spare_size: int) -> bool:
        """Take one permit from the spare pool if fewer than ``spare_size`` are in use."""
        ...

    async def release_spare(self, kit_id: str, token: str) -> None: ...

    async def heartbeat(self, kit_id: str, active: int, tokens: Sequence[str]) -> None:
        """Publish this kit's active-session count and extend its permits."""
        ...

    async def forget(self, kit_id: str) -> None:
        """Remove this kit's count and permits (on shutdown)."""
        ...

    async def snapshot(self, spare_size: int, region_limit: int | None) -> RegionalSnapshot: ...
