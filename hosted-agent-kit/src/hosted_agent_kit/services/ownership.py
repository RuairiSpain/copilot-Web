"""Ownership: one kit schedules each agent (or shard of an agent), and a standby can take over.

``OwnershipManager`` holds a lease for each owned (agent, shard) in an ``OwnershipStore``:

- **Active** kits renew their leases. A kit that cannot confirm its leases for two thirds of the
  time to live stops admitting calls on its own (self-fencing), before the leases can expire and
  another kit start scheduling the same agents.
- **Standby** kits try to claim the leases. When they get all of them they wait a quiet period
  if a different kit held them (its in-flight calls finish), then become active.

Claims are all or nothing, so a kit never owns half of its agents.
"""

from __future__ import annotations

import asyncio
import contextlib
import logging
from collections.abc import Awaitable, Callable, Sequence
from dataclasses import dataclass
from enum import StrEnum

from hosted_agent_kit.config.settings import OwnershipSettings
from hosted_agent_kit.domain.errors import OwnershipConflictError
from hosted_agent_kit.logging_config import log_event
from hosted_agent_kit.ports.ownership import OwnershipStore
from hosted_agent_kit.services.clock import Clock

logger = logging.getLogger(__name__)

FENCE_FRACTION = 2 / 3


class Role(StrEnum):
    STOPPED = "stopped"
    STANDBY = "standby"
    ACTIVE = "active"


@dataclass
class _Held:
    key: str
    epoch: int


def lease_key(project: str, agent: str, shard: int | None) -> str:
    return f"{project}/{agent}/{shard if shard is not None else 0}"


class OwnershipManager:
    def __init__(
        self,
        store: OwnershipStore,
        settings: OwnershipSettings,
        *,
        instance_id: str,
        keys: Sequence[str],
        clock: Clock,
        on_activate: Callable[[], Awaitable[None]],
        on_deactivate: Callable[[], Awaitable[None]],
        sleep: Callable[[float], Awaitable[None]] = asyncio.sleep,
        run_loop: bool = True,
    ) -> None:
        self._run_loop = run_loop  # tests step the manager themselves with ``tick``
        self._store = store
        self._s = settings
        self._instance = instance_id
        self._keys = list(keys)
        self._clock = clock
        self._on_activate = on_activate
        self._on_deactivate = on_deactivate
        self._sleep = sleep
        self._held: list[_Held] = []
        self._activation_lost = False
        self._role = Role.STOPPED
        self._last_confirmed = 0.0
        self._task: asyncio.Task[None] | None = None
        self.takeovers = 0
        self.fences = 0

    @property
    def role(self) -> Role:
        return self._role

    @property
    def epochs(self) -> dict[str, int]:
        return {h.key: h.epoch for h in self._held}

    # ----------------------------------------------------------------- lifecycle

    async def start(self) -> None:
        """Try to become active now. Without ``standby``, a conflict is an error.

        A standby kit that cannot reach the store yet starts anyway and keeps trying.
        """
        self._role = Role.STANDBY
        try:
            await self._store.start()
        except Exception as exc:
            if not self._s.standby:
                self._role = Role.STOPPED
                raise OwnershipConflictError(
                    f"the ownership store is unavailable ({type(exc).__name__})"
                ) from exc
            conflict: str | None = f"the ownership store is unavailable ({type(exc).__name__})"
        else:
            conflict = await self._try_activate()
        if self._role is not Role.ACTIVE and conflict is not None and not self._s.standby:
            self._role = Role.STOPPED
            raise OwnershipConflictError(conflict)
        if self._run_loop:
            self._task = asyncio.create_task(self._loop(), name="ownership")

    async def stop(self) -> None:
        task, self._task = self._task, None
        if task is not None:
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)
        if self._role is Role.ACTIVE:
            await self._on_deactivate()
        await self._release_all()
        self._role = Role.STOPPED
        with contextlib.suppress(Exception):
            await self._store.close()

    async def _loop(self) -> None:
        while True:
            await self._sleep(self._s.renew_seconds)
            try:
                await self.tick()
            except Exception as exc:  # the loop must outlive any one failure
                log_event(
                    logger,
                    "ownership_tick_error",
                    level=logging.ERROR,
                    error_type=type(exc).__name__,
                )

    # ------------------------------------------------------------------- one step

    async def tick(self) -> None:
        """One step: renew when active, try to claim when standby."""
        if self._role is Role.ACTIVE:
            await self._renew_or_fence()
        elif self._role is Role.STANDBY:
            await self._try_activate()

    async def _renew_or_fence(self) -> None:
        started = self._clock.monotonic()
        outcome = await self._renew_all()
        if outcome == "ok":
            # The store set the lease expiry while the call ran, so count from when it began.
            self._last_confirmed = started
            return
        silent_for = self._clock.monotonic() - self._last_confirmed
        # Another try comes one renew period later. Fence now if that would be too late.
        if (
            outcome == "lost"
            or silent_for + self._s.renew_seconds > self._s.ttl_seconds * FENCE_FRACTION
        ):
            await self._fence(outcome)

    async def _fence(self, reason: str) -> None:
        self.fences += 1
        self._role = Role.STANDBY
        log_event(
            logger,
            "ownership_fenced",
            level=logging.ERROR,
            reason=reason,
            instance_id=self._instance,
        )
        try:
            await self._on_deactivate()
        finally:
            self._held.clear()

    async def _try_activate(self) -> str | None:
        """Claim every key. Returns a description of the conflict when one is held elsewhere."""
        claims: list[tuple[str, int, str | None]] = []
        conflict: str | None = None
        for key in self._keys:
            try:
                claim = await self._store.claim(key, self._instance, self._s.ttl_seconds)
            except Exception as exc:
                conflict = f"the ownership store is unavailable ({type(exc).__name__})"
                break
            if not claim.granted:
                conflict = f"{key} is owned by {claim.holder}"
                break
            claims.append((key, claim.epoch, claim.previous))
        if conflict is not None:
            await self._give_back(claims)
            return conflict
        self._held = [_Held(key, epoch) for key, epoch, _ in claims]
        self._last_confirmed = self._clock.monotonic()
        previous = [p for _, _, p in claims if p is not None]
        if previous and self._s.quiet_seconds > 0 and not await self._quiet_wait():
            await self._release_all()
            return "ownership was lost during the quiet period"
        if not await self._activate_while_renewing():
            await self._release_all()
            return "ownership was lost while the kit was starting"
        self._role = Role.ACTIVE
        if previous:
            self.takeovers += 1
            log_event(logger, "ownership_takeover", instance_id=self._instance, previous=previous)
        return None

    async def _activate_while_renewing(self) -> bool:
        """Run ``on_activate`` and keep the leases alive meanwhile: building the runtime and the
        first sync can take as long as a lease. False when a lease was lost during it."""
        keeper = asyncio.create_task(self._renew_during_activation()) if self._run_loop else None
        try:
            await self._on_activate()
        except BaseException:
            await self._release_all()
            raise
        finally:
            if keeper is not None:
                keeper.cancel()
                await asyncio.gather(keeper, return_exceptions=True)
        if self._activation_lost:
            self._activation_lost = False
            await self._on_deactivate()
            return False
        return True

    async def _renew_during_activation(self) -> None:
        while True:
            await self._sleep(self._s.renew_seconds)
            started = self._clock.monotonic()
            outcome = await self._renew_all()
            if outcome == "ok":
                self._last_confirmed = started
                continue
            silent_for = self._clock.monotonic() - self._last_confirmed
            if (
                outcome == "lost"
                or silent_for + self._s.renew_seconds > self._s.ttl_seconds * FENCE_FRACTION
            ):
                self._activation_lost = True
                return

    async def _quiet_wait(self) -> bool:
        """Wait for the previous owner's in-flight calls to finish, renewing the leases."""
        remaining = self._s.quiet_seconds
        while remaining > 0:
            step = min(self._s.renew_seconds, remaining)
            await self._sleep(step)
            remaining -= step
            if await self._renew_all() != "ok":
                return False
            self._last_confirmed = self._clock.monotonic()
        return True

    # -------------------------------------------------------------------- helpers

    async def _renew_all(self) -> str:
        """``ok``, ``lost`` (a lease is held by someone else) or ``error`` (store unreachable)."""
        for held in self._held:
            try:
                renewed = await self._store.renew(
                    held.key, self._instance, held.epoch, self._s.ttl_seconds
                )
            except Exception:
                return "error"
            if not renewed:
                return "lost"
        return "ok"

    async def _give_back(self, claims: Sequence[tuple[str, int, str | None]]) -> None:
        for key, epoch, _ in claims:
            with contextlib.suppress(Exception):
                await self._store.release(key, self._instance, epoch)

    async def _release_all(self) -> None:
        held, self._held = self._held, []
        for item in held:
            with contextlib.suppress(Exception):
                await self._store.release(item.key, self._instance, item.epoch)
