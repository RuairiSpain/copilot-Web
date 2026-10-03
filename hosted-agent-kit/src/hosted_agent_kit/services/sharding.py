"""Sharding one agent across several kits.

Each kit owns a shard of an agent: the users whose hash falls in its index (for stateful agents),
and the sessions whose id starts with its prefix. Ownership is encoded in the session id, so
kits that share a Foundry project need no coordination to know whose session is whose.

Changing ``shard.count`` moves users to other shards, and their derived session ids change with
the shard, so their old sessions are orphaned. Plan a re-shard as a migration.
"""

from __future__ import annotations

import hashlib
import re
import uuid
from collections.abc import Mapping, Sequence
from typing import Generic, TypeVar

PREFIX = "pool-"
_SHARDED = re.compile(r"^pool-s(\d+)-")
T = TypeVar("T")


def shard_for(user_id: str, count: int) -> int:
    """The shard (0 to ``count - 1``) that serves ``user_id``. Stable across processes."""
    if count < 1:
        raise ValueError("shard count must be at least 1")
    digest = hashlib.sha256(b"hosted-agent-kit/shard/v1\x00" + user_id.encode()).digest()
    return int.from_bytes(digest[:8], "big") % count


def shard_prefix(index: int) -> str:
    return f"{PREFIX}s{index}-"


def shard_of(session_id: str) -> int | None:
    """The shard a session id belongs to, or None for an id without a shard prefix."""
    match = _SHARDED.match(session_id)
    return int(match.group(1)) if match else None


def fresh_session_id(index: int) -> str:
    """A caller-chosen id for a new pooled session of shard ``index``."""
    return f"{shard_prefix(index)}{uuid.uuid4().hex}"


class ShardRouter(Generic[T]):  # noqa: UP046 - kept compatible with the supported Python versions
    """Pick the kit (or client) that serves a user, when all the shards are in one process.

    ``shards`` maps shard index to the object that serves it. Use ``shard_for`` with the same
    ``count`` in a load balancer or gateway when the shards are separate processes.
    """

    def __init__(self, shards: Mapping[int, T] | Sequence[T]) -> None:
        items = dict(shards) if isinstance(shards, Mapping) else dict(enumerate(shards))
        if not items or sorted(items) != list(range(len(items))):
            raise ValueError("shards must be numbered 0 to count - 1 with none missing")
        self._shards = items

    @property
    def count(self) -> int:
        return len(self._shards)

    def index_for(self, user_id: str) -> int:
        return shard_for(user_id, self.count)

    def route(self, user_id: str) -> T:
        return self._shards[self.index_for(user_id)]
