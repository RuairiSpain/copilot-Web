"""Connecting to Redis for the optional ledger and ownership store (extra: ``redis``)."""

from __future__ import annotations

import importlib
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


def _entra_provider() -> Any:
    """Microsoft Entra ID tokens for Azure Managed Redis, from the kit's own identity."""
    try:
        provider = importlib.import_module("redis_entraid.cred_provider")
    except ImportError as exc:
        raise ConfigError(
            "entra_auth needs the redis extra: pip install 'hosted-agent-kit[redis]'"
        ) from exc
    return provider.create_from_default_azure_credential(("https://redis.azure.com/.default",))
