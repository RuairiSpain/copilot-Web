"""One owner per agent: leases, standby, takeover, self-fencing and the kit built on them."""

from __future__ import annotations

from typing import Any

import pytest

from hosted_agent_kit import Hack, KitSettings
from hosted_agent_kit.adapters.memory_ownership import MemoryOwnershipStore
from hosted_agent_kit.config.settings import OwnershipSettings
from hosted_agent_kit.domain.errors import NotActiveError, OwnershipConflictError
from hosted_agent_kit.services.ownership import OwnershipManager, Role
from hosted_agent_kit.testing import DemoFoundry
from tests.fakes.clock import FakeClock

TTL = 30.0


def settings(**overrides: Any) -> OwnershipSettings:
    values: dict[str, Any] = {
        "backend": "memory",
        "ttl_seconds": TTL,
        "renew_seconds": 10,
        "quiet_seconds": 20,
    }
    values.update(overrides)
    return OwnershipSettings(**values)


class Rig:
    """A manager with counted activation and a sleep that only advances the fake clock."""

    def __init__(
        self,
        store: MemoryOwnershipStore,
        clock: FakeClock,
        instance: str,
        keys: list[str] | None = None,
        **overrides: Any,
    ) -> None:
        self.activated = 0
        self.deactivated = 0
        self.sleeps: list[float] = []

        async def on_activate() -> None:
            self.activated += 1

        async def on_deactivate() -> None:
            self.deactivated += 1

        async def sleep(seconds: float) -> None:
            self.sleeps.append(seconds)
            clock.advance(seconds)

        self.manager = OwnershipManager(
            store,
            settings(**overrides),
            instance_id=instance,
            keys=keys or ["p/a/0", "p/b/0"],
            clock=clock,
            on_activate=on_activate,
            on_deactivate=on_deactivate,
            sleep=sleep,
            run_loop=False,
        )


def role_of(rig: Rig) -> Role:
    return rig.manager.role


@pytest.fixture
def world() -> tuple[MemoryOwnershipStore, FakeClock]:
    clock = FakeClock()
    return MemoryOwnershipStore(clock), clock


async def test_a_kit_claims_all_its_agents_and_becomes_active(
    world: tuple[MemoryOwnershipStore, FakeClock],
) -> None:
    store, clock = world
    rig = Rig(store, clock, "kit-1")
    await rig.manager.start()
    try:
        assert role_of(rig) is Role.ACTIVE and rig.activated == 1
        assert await store.holder("p/a/0") == "kit-1" == await store.holder("p/b/0")
        assert rig.sleeps == []  # a fresh lease has no previous owner to wait for
    finally:
        await rig.manager.stop()


async def test_a_second_kit_for_the_same_agent_is_refused_and_leaks_nothing(
    world: tuple[MemoryOwnershipStore, FakeClock],
) -> None:
    store, clock = world
    first = Rig(store, clock, "kit-1", keys=["p/b/0"])
    await first.manager.start()
    second = Rig(store, clock, "kit-2")  # wants p/a/0 (free) and p/b/0 (taken)
    try:
        with pytest.raises(OwnershipConflictError, match=r"p/b/0 is owned by kit-1"):
            await second.manager.start()
        assert await store.holder("p/a/0") is None  # the claim it did get was given back
        assert second.activated == 0 and role_of(second) is Role.STOPPED
    finally:
        await first.manager.stop()


async def test_the_owner_keeps_its_leases_by_renewing(
    world: tuple[MemoryOwnershipStore, FakeClock],
) -> None:
    store, clock = world
    owner = Rig(store, clock, "kit-1")
    rival = Rig(store, clock, "kit-2", standby=True)
    await owner.manager.start()
    await rival.manager.start()
    try:
        for _ in range(10):  # five minutes, ten times the lease
            clock.advance(10)
            await owner.manager.tick()
            await rival.manager.tick()
        assert role_of(owner) is Role.ACTIVE and role_of(rival) is Role.STANDBY
        assert rival.activated == 0
    finally:
        await owner.manager.stop()
        await rival.manager.stop()


async def test_a_standby_takes_over_at_once_after_a_graceful_stop(
    world: tuple[MemoryOwnershipStore, FakeClock],
) -> None:
    store, clock = world
    owner = Rig(store, clock, "kit-1")
    standby = Rig(store, clock, "kit-2", standby=True)
    await owner.manager.start()
    await standby.manager.start()
    assert role_of(standby) is Role.STANDBY
    await owner.manager.stop()
    assert owner.deactivated == 1 and role_of(owner) is Role.STOPPED
    await standby.manager.tick()
    try:
        assert role_of(standby) is Role.ACTIVE and standby.activated == 1
        assert standby.sleeps == []  # released leases need no quiet period
    finally:
        await standby.manager.stop()


async def test_a_standby_takes_over_after_a_crash_but_waits_the_quiet_period(
    world: tuple[MemoryOwnershipStore, FakeClock],
) -> None:
    store, clock = world
    owner = Rig(store, clock, "kit-1")
    standby = Rig(store, clock, "kit-2", standby=True)
    await owner.manager.start()  # ... and then it dies without releasing anything
    await standby.manager.start()
    await standby.manager.tick()
    assert role_of(standby) is Role.STANDBY  # the lease has not expired yet
    clock.advance(TTL + 1)
    await standby.manager.tick()
    try:
        assert role_of(standby) is Role.ACTIVE
        assert sum(standby.sleeps) == 20  # the quiet period, in renew-sized steps
        assert standby.manager.takeovers == 1
        assert standby.manager.epochs["p/a/0"] == 2
        assert await store.holder("p/a/0") == "kit-2"  # it kept renewing while it waited
    finally:
        await standby.manager.stop()


async def test_a_kit_that_cannot_confirm_its_leases_stops_serving_before_they_expire(
    world: tuple[MemoryOwnershipStore, FakeClock],
) -> None:
    store, clock = world
    rig = Rig(store, clock, "kit-1")
    await rig.manager.start()
    store.available = False  # the store becomes unreachable
    clock.advance(10)
    await rig.manager.tick()
    assert role_of(rig) is Role.ACTIVE  # one missed renewal is tolerated
    clock.advance(11)  # 21 s without confirmation is past two thirds of the 30 s lease
    await rig.manager.tick()
    assert role_of(rig) is Role.STANDBY and rig.deactivated == 1 and rig.manager.fences == 1
    store.available = True
    await rig.manager.tick()  # the store is back and the lease is still its own
    try:
        assert role_of(rig) is Role.ACTIVE and rig.activated == 2
    finally:
        await rig.manager.stop()


async def test_a_kit_whose_lease_was_taken_stops_serving_immediately(
    world: tuple[MemoryOwnershipStore, FakeClock],
) -> None:
    store, clock = world
    rig = Rig(store, clock, "kit-1")
    await rig.manager.start()
    clock.advance(TTL + 5)  # the kit was paused (a long stall) and its leases expired
    await store.claim("p/a/0", "kit-2", TTL)  # another kit took one of them
    await rig.manager.tick()
    assert role_of(rig) is Role.STANDBY and rig.deactivated == 1


async def test_start_when_the_store_is_down_is_a_conflict_unless_standby(
    world: tuple[MemoryOwnershipStore, FakeClock],
) -> None:
    store, clock = world
    store.available = False
    with pytest.raises(OwnershipConflictError, match="unavailable"):
        await Rig(store, clock, "kit-1").manager.start()
    waiting = Rig(store, clock, "kit-2", standby=True)
    await waiting.manager.start()
    assert role_of(waiting) is Role.STANDBY
    store.available = True
    await waiting.manager.tick()
    try:
        assert role_of(waiting) is Role.ACTIVE
    finally:
        await waiting.manager.stop()


async def test_a_failed_activation_gives_the_leases_back(
    world: tuple[MemoryOwnershipStore, FakeClock],
) -> None:
    store, clock = world
    rig = Rig(store, clock, "kit-1")

    async def broken() -> None:
        raise RuntimeError("cannot start")

    rig.manager._on_activate = broken
    with pytest.raises(RuntimeError, match="cannot start"):
        await rig.manager.start()
    assert await store.holder("p/a/0") is None


def test_ownership_settings_are_checked() -> None:
    from hosted_agent_kit.config.models import ConfigError

    with pytest.raises(ConfigError, match="half"):
        OwnershipSettings(backend="memory", ttl_seconds=10, renew_seconds=6)
    with pytest.raises(ConfigError, match="url"):
        OwnershipSettings(backend="redis")


# ---------------------------------------------------------------- the kit itself

DOC = {
    "agentPool": {
        "agents": {
            "chat": {"mode": "stateless"},
            "memo": {"mode": "stateful"},
            "docs": {"mode": "stateless"},
        }
    }
}


def kit_settings(**overrides: Any) -> KitSettings:
    values: dict[str, Any] = {
        "ownership": {"backend": "memory", "ttl_seconds": 30, "renew_seconds": 10},
        "startup_sync_timeout_seconds": 5,
    }
    values.update(overrides)
    return KitSettings(**values)


async def test_two_kits_cannot_serve_the_same_agents_and_a_standby_takes_over() -> None:
    clock = FakeClock()
    store = MemoryOwnershipStore(clock)
    first = Hack.from_dict(
        DOC, adapter=DemoFoundry(), clock=clock, ownership_store=store, settings=kit_settings()
    )
    await first.start()
    assert first.role == "active" and first.ready
    assert (await first.ask("chat", "hi", user_id="u")).ok

    refused = Hack.from_dict(
        DOC, adapter=DemoFoundry(), clock=clock, ownership_store=store, settings=kit_settings()
    )
    with pytest.raises(OwnershipConflictError):
        await refused.start()

    standby = Hack.from_dict(
        DOC,
        adapter=DemoFoundry(),
        clock=clock,
        ownership_store=store,
        settings=kit_settings(
            ownership={
                "backend": "memory",
                "ttl_seconds": 30,
                "renew_seconds": 10,
                "standby": True,
                "quiet_seconds": 0,
            }
        ),
    )
    await standby.start()
    try:
        assert standby.role == "standby" and not standby.ready
        with pytest.raises(NotActiveError) as info:
            await standby.ask("chat", "hi", user_id="u")
        assert info.value.status == 503 and info.value.retry_safe
        with pytest.raises(NotActiveError):
            standby.reporting
        await first.stop()  # releases its leases
        assert standby.ownership is not None
        await standby.ownership.tick()
        assert standby.role == "active" and standby.ready
        assert (await standby.ask("chat", "again", user_id="u")).ok
    finally:
        await standby.stop()


async def test_owns_limits_the_agents_a_kit_schedules() -> None:
    kit = Hack.from_dict(DOC, adapter=DemoFoundry(), settings=KitSettings(owns=["chat", "docs"]))
    async with kit:
        assert kit.agent_names == ["chat", "docs"]
        from hosted_agent_kit.domain.errors import AgentNotConfiguredError

        with pytest.raises(AgentNotConfiguredError):
            await kit.ask("memo", "hi", user_id="u")


async def test_owns_must_name_configured_agents() -> None:
    from hosted_agent_kit.config.models import ConfigError

    kit = Hack.from_dict(DOC, adapter=DemoFoundry(), settings=KitSettings(owns=["nope"]))
    with pytest.raises(ConfigError, match="not configured"):
        await kit.start()
    empty = Hack.from_dict(DOC, adapter=DemoFoundry(), settings=KitSettings(owns=[]))
    with pytest.raises(ConfigError, match="at least one"):
        await empty.start()


async def test_two_kits_with_different_owns_run_side_by_side_on_one_store() -> None:
    clock = FakeClock()
    store = MemoryOwnershipStore(clock)
    team_a = Hack.from_dict(
        DOC,
        adapter=DemoFoundry(),
        clock=clock,
        ownership_store=store,
        settings=kit_settings(owns=["chat"], kit_id="team-a"),
    )
    team_b = Hack.from_dict(
        DOC,
        adapter=DemoFoundry(),
        clock=clock,
        ownership_store=store,
        settings=kit_settings(owns=["memo", "docs"], kit_id="team-b"),
    )
    await team_a.start()
    await team_b.start()
    try:
        assert (await team_a.ask("chat", "x", user_id="u")).ok
        assert (await team_b.ask("memo", "x", user_id="u")).ok
        assert team_a.instance_id.startswith("team-a-") and team_a.instance_id != team_b.instance_id
    finally:
        await team_a.stop()
        await team_b.stop()
