"""Per-user isolation keys for Foundry's ``x-ms-user-isolation-key`` header.

Foundry scopes per-user resources (OAuth consent tokens, memory stores, response chains) by this
key. It is derived with a keyed hash so the raw user id is never stored in Foundry's partitioning
and the same user always gets the same key, across restarts and across kits that share the secret.
"""

from __future__ import annotations

import hashlib
import hmac

_LABEL = b"hosted-agent-kit/user-isolation/v1\x00"
_DIGEST_CHARS = 32


class UserIsolationKeys:
    def __init__(self, secret: bytes) -> None:
        self._secret = secret

    def key_for(self, user_id: str) -> str:
        digest = hmac.new(self._secret, _LABEL + user_id.encode(), hashlib.sha256).hexdigest()
        return digest[:_DIGEST_CHARS]
