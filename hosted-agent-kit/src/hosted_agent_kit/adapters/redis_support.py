"""Connecting to Redis for the optional ledger and ownership store (extra: ``redis``)."""

from __future__ import annotations

import asyncio
import base64
import importlib
import json
import time
from collections.abc import Callable
from typing import Any

from pydantic import SecretStr

from hosted_agent_kit.config.models import ConfigError


def connect(
    url: SecretStr | None, *, entra_auth: bool, timeout_seconds: float, client: Any | None = None
) -> Any:
    """An asyncio Redis client. ``client`` lets tests pass a fake."""
    if client is not None:
        return client
    if url is None:
        raise ConfigError("a Redis url is required (set it in the environment)")
    try:
        redis_asyncio = importlib.import_module("redis.asyncio")
    except ImportError as exc:
        raise ConfigError(
            "Redis support needs the redis extra: pip install 'hosted-agent-kit[redis]'"
        ) from exc
    options: dict[str, Any] = {
        "socket_timeout": timeout_seconds,
        "socket_connect_timeout": timeout_seconds,
        "health_check_interval": 30,
        "decode_responses": True,
    }
    if entra_auth:
        options["credential_provider"] = _entra_provider()
    return redis_asyncio.Redis.from_url(url.get_secret_value(), **options)


SCOPE = "https://redis.azure.com/.default"
REFRESH_MARGIN_SECONDS = 300


class EntraCredentialProvider:
    """Microsoft Entra ID tokens for Azure Managed Redis, from the kit's own identity.

    Redis takes the token as the password and the identity's object id (the token's ``oid``
    claim) as the user name. A token is reused until five minutes before it expires. Azure Redis
    closes a connection when its token expires; the next connection asks for a fresh token here.
    This replaces the ``redis-entraid`` package, which pins an old PyJWT with known advisories.
    """

    def __init__(self, credential: Any, *, clock: Callable[[], float] = time.time) -> None:
        self._credential = credential
        self._clock = clock
        self._cached: tuple[str, str, float] | None = None  # user, token, expires_on

    def get_credentials(self) -> tuple[str, str]:
        cached = self._cached
        if cached is None or cached[2] - self._clock() < REFRESH_MARGIN_SECONDS:
            token = self._credential.get_token(SCOPE)
            cached = (_object_id(token.token), token.token, float(token.expires_on))
            self._cached = cached
        return cached[0], cached[1]

    async def get_credentials_async(self) -> tuple[str, str]:
        # Acquiring a token can block on the network, so keep it off the event loop.
        return await asyncio.to_thread(self.get_credentials)


def _object_id(token: str) -> str:
    """The ``oid`` claim of an access token. Read only: Redis validates the token itself."""
    try:
        payload = token.split(".")[1]
        claims = json.loads(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)))
        return str(claims["oid"])
    except (IndexError, ValueError, KeyError) as exc:
        raise ValueError("the Entra token has no object id (oid) claim") from exc


def _entra_provider() -> Any:
    try:
        identity = importlib.import_module("azure.identity")
        provider = importlib.import_module("redis.credentials").CredentialProvider
    except ImportError as exc:
        raise ConfigError(
            "entra_auth needs the redis extra: pip install 'hosted-agent-kit[redis]'"
        ) from exc
    # Subclass redis-py's base so its client accepts the object.
    return type("EntraRedisProvider", (EntraCredentialProvider, provider), {})(
        identity.DefaultAzureCredential()
    )
