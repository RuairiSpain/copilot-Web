"""Deterministic session ids for stateful agents.

Foundry lets the caller choose a session id when it creates a session. Deriving the id from the
user and agent with a keyed hash means the pool can find a user's session again after a restart,
without storing anything. The key keeps ids unguessable and stops anyone reading the Foundry
portal from linking a session id to a user id. Only the service holds the key.
"""

from __future__ import annotations

import hashlib
import hmac
import re

PREFIX = "pool-"
_DIGEST_CHARS = 40
_PATTERN = re.compile(rf"^{PREFIX}(s\d+-)?[0-9a-f]{{{_DIGEST_CHARS}}}$")


class SessionIdDeriver:
    def __init__(self, key: bytes, *, shard: int | None = None) -> None:
        self._key = key
        self._shard = shard  # a sharded kit puts its shard in every id it derives

    def for_user(self, agent_name: str, user_id: str, conversation_key: str | None = None) -> str:
        """A conversation key gives the same user another session. No key, same id as before."""
        parts = [agent_name, user_id]
        if conversation_key is not None:
            parts.append(conversation_key)
        message = "\x00".join(parts).encode()
        digest = hmac.new(self._key, message, hashlib.sha256).hexdigest()
        prefix = PREFIX if self._shard is None else f"{PREFIX}s{self._shard}-"
        return prefix + digest[:_DIGEST_CHARS]

    @staticmethod
    def is_derived(session_id: str) -> bool:
        """True for an id in the derived format. Says nothing about which user owns it."""
        return _PATTERN.match(session_id) is not None
