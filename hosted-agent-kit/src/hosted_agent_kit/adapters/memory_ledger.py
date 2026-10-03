"""In-process ``QuotaLedger``: for tests, and for several kits in one process."""

from __future__ import annotations

from collections.abc import Sequence

from hosted_agent_kit.ports.quota import RegionalSnapshot
from hosted_agent_kit.services.clock import Clock


class MemoryLedger:
    def __init__(self, clock: Clock, *, ttl_seconds: float = 30) -> None:
        self._clock = clock
        self._ttl = ttl_seconds
        self._permits: dict[str, float] = {}  # "kit_id|token" -> expires (monotonic)
        self._kits: dict[str, tuple[int, float]] = {}  # kit_id -> (active, expires)
        self.available = True  # tests set this to False to simulate an outage

    async def start(self) -> None:
        return None

    async def close(self) -> None:
        return None

    def _check(self) -> None:
        if not self.available:
            raise ConnectionError("ledger unavailable")

    def _purge(self) -> None:
        now = self._clock.monotonic()
        self._permits = {k: v for k, v in self._permits.items() if v > now}
        self._kits = {k: v for k, v in self._kits.items() if v[1] > now}

    async def acquire_spare(self, kit_id: str, token: str, spare_size: int) -> bool:
        self._check()
        self._purge()
        if len(self._permits) >= spare_size:
            return False
        self._permits[f"{kit_id}|{token}"] = self._clock.monotonic() + self._ttl
        return True

    async def release_spare(self, kit_id: str, token: str) -> None:
        self._check()
        self._permits.pop(f"{kit_id}|{token}", None)

    async def heartbeat(self, kit_id: str, active: int, tokens: Sequence[str]) -> list[str]:
        self._check()
        self._purge()
        expires = self._clock.monotonic() + self._ttl
        self._kits[kit_id] = (active, expires)
        lost: list[str] = []
        for token in tokens:
            key = f"{kit_id}|{token}"
            if key in self._permits:
                self._permits[key] = expires
            else:
                lost.append(token)
        return lost

    async def forget(self, kit_id: str) -> None:
        self._check()
        self._kits.pop(kit_id, None)
        self._permits = {k: v for k, v in self._permits.items() if not k.startswith(f"{kit_id}|")}

    async def snapshot(self, spare_size: int, region_limit: int | None) -> RegionalSnapshot:
        self._check()
        self._purge()
        kits = {kit: active for kit, (active, _) in self._kits.items()}
        return RegionalSnapshot(
            region_limit=region_limit,
            total_active=sum(kits.values()),
            spare_size=spare_size,
            spare_used=len(self._permits),
            kits=kits,
        )
