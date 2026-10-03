"""The Redis ownership store and quota ledger, on an in-memory Redis with Lua support."""

from __future__ import annotations

import asyncio
from collections.abc import AsyncIterator
from typing import Any

import pytest
from fakeredis import FakeAsyncRedis

from hosted_agent_kit.adapters.redis_ledger import RedisLedger
from hosted_agent_kit.adapters.redis_ownership import RedisOwnershipStore


@pytest.fixture
async def client() -> AsyncIterator[Any]:
    redis = FakeAsyncRedis(decode_responses=True)
    yield redis
    await redis.aclose()


# ------------------------------------------------------------------ ownership


async def test_a_free_lease_is_granted_and_a_held_one_is_refused(client: Any) -> None:
    store = RedisOwnershipStore(client)
    first = await store.claim("p/a/0", "kit-1", 5)
    assert first.granted and first.epoch == 1 and first.previous is None
    second = await store.claim("p/a/0", "kit-2", 5)
    assert not second.granted and second.holder == "kit-1" and second.epoch == 1
    assert await store.holder("p/a/0") == "kit-1"


async def test_reclaiming_ones_own_lease_renews_it(client: Any) -> None:
    store = RedisOwnershipStore(client)
    first = await store.claim("k", "kit-1", 5)
    again = await store.claim("k", "kit-1", 5)
    assert again.granted and again.epoch == first.epoch and again.previous is None


async def test_renew_needs_the_current_holder_and_epoch(client: Any) -> None:
    store = RedisOwnershipStore(client)
    claim = await store.claim("k", "kit-1", 5)
    assert await store.renew("k", "kit-1", claim.epoch, 5)
    assert not await store.renew("k", "kit-2", claim.epoch, 5)
    assert not await store.renew("k", "kit-1", claim.epoch + 1, 5)


async def test_an_expired_lease_is_taken_over_with_a_new_epoch_and_the_old_holder_named(
    client: Any,
) -> None:
    store = RedisOwnershipStore(client)
    first = await store.claim("k", "kit-1", 0.1)
    await asyncio.sleep(0.2)
    assert await store.holder("k") is None
    taken = await store.claim("k", "kit-2", 5)
    assert taken.granted and taken.epoch == first.epoch + 1 and taken.previous == "kit-1"
    assert not await store.renew("k", "kit-1", first.epoch, 5)  # the old holder cannot renew


async def test_a_graceful_release_leaves_no_previous_holder(client: Any) -> None:
    store = RedisOwnershipStore(client)
    claim = await store.claim("k", "kit-1", 5)
    await store.release("k", "kit-1", claim.epoch)
    assert await store.holder("k") is None
    taken = await store.claim("k", "kit-2", 5)
    assert taken.granted and taken.previous is None  # nothing in flight to wait for


async def test_release_by_a_non_holder_does_nothing(client: Any) -> None:
    store = RedisOwnershipStore(client)
    claim = await store.claim("k", "kit-1", 5)
    await store.release("k", "kit-2", claim.epoch)
    assert await store.holder("k") == "kit-1"


async def test_a_holder_id_with_a_bar_is_refused(client: Any) -> None:
    with pytest.raises(ValueError, match="cannot contain"):
        await RedisOwnershipStore(client).claim("k", "a|b", 5)


# --------------------------------------------------------------------- ledger


def ledger(client: Any, ttl: float = 5) -> RedisLedger:
    return RedisLedger(client, scope="sub1/swedencentral", ttl_seconds=ttl)


async def test_spare_permits_are_limited_to_the_pool_size(client: Any) -> None:
    pool = ledger(client)
    assert await pool.acquire_spare("kit-1", "t1", 2)
    assert await pool.acquire_spare("kit-2", "t2", 2)
    assert not await pool.acquire_spare("kit-3", "t3", 2)
    await pool.release_spare("kit-1", "t1")
    assert await pool.acquire_spare("kit-3", "t3", 2)


async def test_permits_expire_without_a_heartbeat_and_survive_with_one(client: Any) -> None:
    pool = ledger(client, ttl=0.3)
    assert await pool.acquire_spare("kit-1", "t1", 1)
    assert await pool.acquire_spare("kit-2", "t2", 2)
    for _ in range(3):
        await asyncio.sleep(0.15)
        await pool.heartbeat("kit-1", 4, ["t1"])  # kit-1 keeps its permit alive
    snapshot = await pool.snapshot(spare_size=2, region_limit=100)
    assert snapshot.spare_used == 1  # kit-2's permit expired
    assert not await pool.acquire_spare("kit-3", "t3", 1)  # kit-1's still holds the only one


async def test_the_snapshot_sums_live_kits_and_drops_dead_ones(client: Any) -> None:
    pool = ledger(client, ttl=0.3)
    await pool.heartbeat("kit-1", 30, [])
    await pool.heartbeat("kit-2", 12, [])
    snapshot = await pool.snapshot(spare_size=10, region_limit=100)
    assert snapshot.kits == {"kit-1": 30, "kit-2": 12}
    assert snapshot.total_active == 42 and snapshot.headroom == 58
    await asyncio.sleep(0.4)
    assert (await pool.snapshot(10, 100)).kits == {}


async def test_forget_removes_a_kits_count_and_permits(client: Any) -> None:
    pool = ledger(client)
    await pool.acquire_spare("kit-1", "t1", 5)
    await pool.acquire_spare("kit-2", "t2", 5)
    await pool.heartbeat("kit-1", 7, ["t1"])
    await pool.heartbeat("kit-2", 3, ["t2"])
    await pool.forget("kit-1")
    snapshot = await pool.snapshot(5, None)
    assert snapshot.kits == {"kit-2": 3} and snapshot.spare_used == 1
    assert snapshot.region_limit is None and snapshot.headroom is None


async def test_scopes_do_not_share_state(client: Any) -> None:
    one = RedisLedger(client, scope="sub1/a", ttl_seconds=5)
    two = RedisLedger(client, scope="sub1/b", ttl_seconds=5)
    await one.heartbeat("kit-1", 9, [])
    assert (await two.snapshot(1, None)).kits == {}
