"""Per-user isolation keys and the acting-user header."""

from __future__ import annotations

import pytest

from hosted_agent_kit.config.models import ConfigError
from hosted_agent_kit.runtime import isolation_keys
from hosted_agent_kit.services.isolation import UserIsolationKeys
from tests.conftest import make_config, make_harness, make_request, make_settings

SECRET = "s" * 32


def test_keys_are_stable_distinct_and_do_not_contain_the_user_id() -> None:
    keys = UserIsolationKeys(SECRET.encode())
    assert keys.key_for("alice") == keys.key_for("alice")
    assert keys.key_for("alice") != keys.key_for("bob")
    assert "alice" not in keys.key_for("alice") and len(keys.key_for("alice")) == 32
    other = UserIsolationKeys(("t" * 32).encode())
    assert other.key_for("alice") != keys.key_for("alice")  # a different secret, a different key


def test_isolation_keys_need_a_secret_only_when_an_agent_uses_them() -> None:
    plain = make_config({"a": {"mode": "stateless"}})
    uses = make_config({"a": {"mode": "stateless", "user_isolation": "key"}})
    assert isolation_keys(make_settings(), plain) is None
    with pytest.raises(ConfigError, match="user_isolation"):
        isolation_keys(make_settings(), uses)
    assert isolation_keys(make_settings(user_isolation_secret=SECRET), uses) is not None
    assert isolation_keys(make_settings(session_id_key=SECRET), uses) is not None  # the fallback


def test_a_short_secret_is_refused() -> None:
    with pytest.raises(ConfigError, match="USER_ISOLATION_SECRET"):
        make_settings(user_isolation_secret="short")


async def test_off_sends_nothing_per_user() -> None:
    h = make_harness(defaults={"mode": "stateless", "user_isolation": "off"})
    await h.pool.execute(make_request())
    context = h.fake.invocations[-1]
    assert context.isolation_key is None and context.acting_user is None


async def test_key_mode_sends_the_derived_key_for_each_user() -> None:
    h = make_harness(
        defaults={"mode": "stateless", "max_sessions": 3, "user_isolation": "key"},
        user_isolation_secret=SECRET,
    )
    await h.pool.execute(make_request(user="alice"))
    alice = h.fake.invocations[-1]
    await h.pool.execute(make_request(user="bob"))
    bob = h.fake.invocations[-1]
    keys = UserIsolationKeys(SECRET.encode())
    assert alice.isolation_key == keys.key_for("alice") and alice.acting_user is None
    assert bob.isolation_key == keys.key_for("bob")


async def test_delegated_mode_also_names_the_acting_user() -> None:
    h = make_harness(
        defaults={"mode": "stateless", "user_isolation": "delegated"},
        user_isolation_secret=SECRET,
    )
    await h.pool.execute(make_request(user="alice"))
    context = h.fake.invocations[-1]
    assert context.acting_user == "alice" and context.isolation_key is not None
