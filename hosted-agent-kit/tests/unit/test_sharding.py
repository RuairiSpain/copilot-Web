"""One agent shared by several kits: users and sessions are partitioned by shard."""

from __future__ import annotations

from collections import Counter
from typing import Any

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient

from hosted_agent_kit import Hack, KitSettings
from hosted_agent_kit.config.settings import ShardSettings
from hosted_agent_kit.domain.errors import WrongShardError
from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.services.session_ids import SessionIdDeriver
from hosted_agent_kit.services.sharding import (
    ShardRouter,
    fresh_session_id,
    shard_for,
    shard_of,
    shard_prefix,
)
from hosted_agent_kit.testing import DemoFoundry, FakeClock

DOC = {
    "agentPool": {
        "defaults": {"max_sessions": 6},
        "agents": {"chat": {"mode": "stateless"}, "memo": {"mode": "stateful"}},
    }
}
SECRET = "k" * 32


def sharded(index: int, count: int, foundry: DemoFoundry, **overrides: Any) -> Hack:
    settings = KitSettings(
        shard={"index": index, "count": count},
        session_id_key=SECRET,
        startup_sync_timeout_seconds=5,
        **overrides,
    )
    return Hack.from_dict(DOC, adapter=foundry, settings=settings, clock=FakeClock())


# ----------------------------------------------------------------------- helpers


def test_shard_for_is_stable_in_range_and_spread_evenly() -> None:
    assert shard_for("alice", 4) == shard_for("alice", 4)
    counts = Counter(shard_for(f"user-{i}", 4) for i in range(4000))
    assert set(counts) == {0, 1, 2, 3}
    assert all(800 < n < 1200 for n in counts.values())  # about a quarter each
    assert shard_for("alice", 1) == 0
    with pytest.raises(ValueError, match="at least 1"):
        shard_for("alice", 0)


def test_session_ids_carry_their_shard() -> None:
    assert shard_prefix(3) == "pool-s3-"
    assert shard_of(fresh_session_id(2)) == 2
    assert shard_of("pool-" + "a" * 40) is None  # an id from an unsharded kit
    assert shard_of("sess-0001") is None
    derived = SessionIdDeriver(SECRET.encode(), shard=1).for_user("memo", "alice")
    assert shard_of(derived) == 1 and SessionIdDeriver.is_derived(derived)
    unsharded = SessionIdDeriver(SECRET.encode()).for_user("memo", "alice")
    assert SessionIdDeriver.is_derived(unsharded) and derived != unsharded


def test_router_picks_the_kit_for_a_user() -> None:
    router = ShardRouter(["zero", "one", "two"])
    assert router.count == 3
    assert router.route("alice") == ["zero", "one", "two"][shard_for("alice", 3)]
    assert ShardRouter({0: "a", 1: "b"}).route("bob") in ("a", "b")
    with pytest.raises(ValueError, match="numbered"):
        ShardRouter({0: "a", 2: "c"})
    with pytest.raises(ValueError, match="numbered"):
        ShardRouter([])


def test_shard_settings_are_checked() -> None:
    from hosted_agent_kit.config.models import ConfigError

    with pytest.raises(ConfigError, match="less than"):
        ShardSettings(index=2, count=2)


# --------------------------------------------------------------------- the kits


async def test_a_sharded_kit_creates_sessions_whose_ids_carry_its_shard() -> None:
    foundry = DemoFoundry()
    kit = sharded(1, 3, foundry)
    async with kit:
        await kit.ask("chat", "hi", user_id="u")
        (created,) = foundry.created
        assert shard_of(created) == 1


async def test_a_stateful_kit_serves_only_its_users_and_names_the_right_shard() -> None:
    foundry = DemoFoundry()
    kit = sharded(0, 2, foundry)
    mine = next(u for u in (f"u{i}" for i in range(50)) if shard_for(u, 2) == 0)
    other = next(u for u in (f"u{i}" for i in range(50)) if shard_for(u, 2) == 1)
    async with kit:
        assert (await kit.ask("memo", "hi", user_id=mine)).ok
        assert shard_of(foundry.created[0]) == 0  # the derived id carries the shard
        with pytest.raises(WrongShardError) as info:
            await kit.ask("memo", "hi", user_id=other)
        assert (info.value.shard, info.value.count, info.value.status) == (1, 2, 421)
        assert kit.shard_for(other) == 1
        assert (await kit.ask("chat", "hi", user_id=other)).ok  # a stateless agent: any shard


async def test_the_wrong_shard_answers_421_with_the_shard_in_the_body() -> None:
    foundry = DemoFoundry()
    kit = sharded(0, 2, foundry)
    other = next(u for u in (f"u{i}" for i in range(50)) if shard_for(u, 2) == 1)
    app = FastAPI()
    install(app, kit)

    @app.post("/ask")
    async def ask(user: str, kit: KitDep) -> Any:
        return (await kit.ask("memo", "hi", user_id=user)).json()

    with TestClient(app) as client:
        response = client.post("/ask", params={"user": other})
    body = response.json()
    assert response.status_code == 421
    assert body["error_code"] == "WRONG_SHARD" and body["shard"] == 1 and body["shard_count"] == 2


async def test_each_shard_adopts_only_its_own_sessions() -> None:
    foundry = DemoFoundry()
    mine = foundry.add_session("chat", session_id=fresh_session_id(0))
    theirs = foundry.add_session("chat", session_id=fresh_session_id(1))
    unprefixed = foundry.add_session("chat", session_id="legacy-session")
    kit0 = sharded(0, 2, foundry)
    kit1 = sharded(1, 2, foundry)
    async with kit0, kit1:
        ids0 = {s.session_id for s in await kit0.reporting.sessions("chat")}
        ids1 = {s.session_id for s in await kit1.reporting.sessions("chat")}
    assert ids0 == {mine.session_id, unprefixed.session_id}  # shard 0 also takes unprefixed ids
    assert ids1 == {theirs.session_id}


async def test_a_derived_session_of_another_shard_is_not_adopted() -> None:
    foundry = DemoFoundry()
    other = SessionIdDeriver(SECRET.encode(), shard=1).for_user("memo", "alice")
    foundry.add_session("memo", session_id=other)
    kit0 = sharded(0, 2, foundry)
    async with kit0:
        assert await kit0.reporting.sessions("memo") == []
