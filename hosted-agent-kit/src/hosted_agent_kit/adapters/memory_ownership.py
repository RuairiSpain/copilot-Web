"""In-process ``OwnershipStore``: for tests, and for several kits in one process."""

from __future__ import annotations

from dataclasses import dataclass

from hosted_agent_kit.ports.ownership import Claim
from hosted_agent_kit.services.clock import Clock


@dataclass
class _Lease:
    holder: str
    epoch: int
    expires: float


class MemoryOwnershipStore:
    def __init__(self, clock: Clock) -> None:
        self._clock = clock
        self._leases: dict[str, _Lease] = {}
        self._last: dict[str, str] = {}  # the last holder, kept after the lease expires
        self._epochs: dict[str, int] = {}
        self.available = True  # tests set this to False to simulate an outage

    async def start(self) -> None:
        return None

    async def close(self) -> None:
        return None

    def _check(self) -> None:
        if not self.available:
            raise ConnectionError("ownership store unavailable")

    def _live(self, key: str) -> _Lease | None:
        lease = self._leases.get(key)
        if lease is not None and lease.expires <= self._clock.monotonic():
            return None
        return lease

    async def claim(self, key: str, holder: str, ttl_seconds: float) -> Claim:
        self._check()
        live = self._live(key)
        if live is not None and live.holder != holder:
            return Claim(False, live.holder, live.epoch)
        expires = self._clock.monotonic() + ttl_seconds
        if live is not None:  # the caller's own lease: renew it
            live.expires = expires
            return Claim(True, holder, live.epoch)
        previous = self._last.get(key)
        epoch = self._epochs.get(key, 0)
        if previous != holder:
            epoch += 1
            self._epochs[key] = epoch
        self._leases[key] = _Lease(holder, epoch, expires)
        self._last[key] = holder
        return Claim(True, holder, epoch, previous if previous != holder else None)

    async def renew(self, key: str, holder: str, epoch: int, ttl_seconds: float) -> bool:
        self._check()
        live = self._live(key)
        if live is None or live.holder != holder or live.epoch != epoch:
            return False
        live.expires = self._clock.monotonic() + ttl_seconds
        return True

    async def release(self, key: str, holder: str, epoch: int) -> None:
        self._check()
        live = self._live(key)
        if live is not None and live.holder == holder and live.epoch == epoch:
            del self._leases[key]
            self._last.pop(key, None)  # a graceful release: the next holder need not wait

    async def holder(self, key: str) -> str | None:
        self._check()
        live = self._live(key)
        return live.holder if live is not None else None
