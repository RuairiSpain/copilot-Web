"""Port for ownership leases: which kit schedules which agent (or shard of an agent).

A lease has a holder, an epoch that grows each time a different holder takes it, and a time to
live. The holder renews it while it is healthy. When it stops renewing, the lease expires and a
standby can take over. ``previous`` tells a new holder that someone held the lease before, so it
can wait for that holder's in-flight work to finish.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol


@dataclass(frozen=True)
class Claim:
    granted: bool
    holder: str  # who holds the lease now (the caller when granted)
    epoch: int
    previous: str | None = None  # the earlier holder, when this claim took the lease from one


class OwnershipStore(Protocol):
    async def start(self) -> None: ...

    async def close(self) -> None: ...

    async def claim(self, key: str, holder: str, ttl_seconds: float) -> Claim:
        """Take the lease if it is free or expired. Re-claiming one's own lease renews it."""
        ...

    async def renew(self, key: str, holder: str, epoch: int, ttl_seconds: float) -> bool:
        """Extend the lease if ``holder`` still holds it at ``epoch``. False means it was lost."""
        ...

    async def release(self, key: str, holder: str, epoch: int) -> None:
        """Give the lease up now, if still held at ``epoch``."""
        ...

    async def holder(self, key: str) -> str | None:
        """The current holder, or None when the lease is free or expired."""
        ...
