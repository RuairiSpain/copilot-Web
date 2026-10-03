"""Settings read from the environment (never from YAML).

``KitSettings`` holds what the scheduling runtime needs and is what an application that embeds
the kit uses. ``Settings`` adds the options of the standalone HTTP service (caller
authentication, request limits, operations endpoints).
"""

from __future__ import annotations

from pathlib import Path
from typing import Annotated, Literal, Self

from pydantic import AliasChoices, Field, SecretStr, field_validator, model_validator
from pydantic_settings import BaseSettings, NoDecode, SettingsConfigDict

from hosted_agent_kit.config.models import ConfigError

MIN_SESSION_KEY_CHARS = 32


class KitSettings(BaseSettings):
    """Runtime settings shared by the embedded kit and the standalone service."""

    model_config = SettingsConfigDict(env_prefix="POOL_", extra="ignore", populate_by_name=True)

    # Locations and Foundry connection
    agent_pool_config: Path | None = Field(
        default=None, validation_alias=AliasChoices("AGENT_POOL_CONFIG", "POOL_AGENT_POOL_CONFIG")
    )
    foundry_project_endpoint: str | None = Field(
        default=None,
        validation_alias=AliasChoices("FOUNDRY_PROJECT_ENDPOINT", "POOL_FOUNDRY_PROJECT_ENDPOINT"),
    )
    applicationinsights_connection_string: str | None = Field(
        default=None,
        validation_alias=AliasChoices(
            "APPLICATIONINSIGHTS_CONNECTION_STRING", "POOL_APPLICATIONINSIGHTS_CONNECTION_STRING"
        ),
    )
    foundry_isolation_key: str | None = Field(
        default=None,
        description="Constant x-ms-user-isolation-key sent to Foundry. Only agents that use the "
        "Header authorisation scheme need it.",
    )
    session_id_key: SecretStr | None = Field(
        default=None,
        description="Secret that derives each stateful user's session id. Setting it lets the "
        "pool find users' sessions again after a restart. Keep it stable.",
    )

    # Timeouts and limits
    default_timeout_seconds: float = Field(default=120, gt=0)
    max_timeout_seconds: float = Field(default=900, gt=0)
    max_stream_seconds: float = Field(default=600, gt=0)
    # Optional ceiling on the whole of a request before streaming starts: queue wait, session
    # creation, backoff and every invocation attempt. ``timeout_seconds`` applies to one
    # upstream call only. Unset means no overall ceiling.
    max_request_seconds: float | None = Field(default=None, gt=0)
    # Largest non-streaming Invocations response the pool will buffer.
    max_response_bytes: int = Field(default=16_777_216, ge=1)
    # Deduplication of requests that carry an Idempotency-Key. 0 turns it off.
    idempotency_ttl_seconds: float = Field(default=600, ge=0)
    idempotency_max_entries: int = Field(default=1000, ge=1)
    idempotency_max_body_bytes: int = Field(default=1_048_576, ge=1)
    # A capacity reservation that has not been used or released after this long is expired.
    reservation_ttl_seconds: float = Field(default=1800, gt=0)
    # On shutdown the runtime stops admitting requests and waits this long for running ones.
    shutdown_grace_seconds: float = Field(default=30, ge=0)
    retry_after_seconds: int = Field(default=10, ge=1)
    # The SDK waits this long at start for the first sync with Foundry, then carries on.
    startup_sync_timeout_seconds: float = Field(default=30, ge=0)

    # Upstream behaviour
    upstream_throttle_retries: int = Field(default=2, ge=0, le=10)
    delete_retries: int = Field(default=3, ge=0, le=10)
    create_ready_timeout_seconds: float = Field(default=120, gt=0)
    backoff_base_seconds: float = Field(default=0.5, gt=0)
    backoff_max_seconds: float = Field(default=30, gt=0)

    log_level: str = "INFO"
    service_name: str = "hosted-agent-kit"

    @model_validator(mode="after")
    def _check_session_key(self) -> Self:
        key = self.session_id_key
        if key is not None and len(key.get_secret_value()) < MIN_SESSION_KEY_CHARS:
            raise ConfigError(
                f"POOL_SESSION_ID_KEY must be at least {MIN_SESSION_KEY_CHARS} characters"
            )
        return self

    @model_validator(mode="after")
    def _check_timeouts(self) -> Self:
        if self.default_timeout_seconds > self.max_timeout_seconds:
            raise ConfigError("POOL_DEFAULT_TIMEOUT_SECONDS cannot exceed POOL_MAX_TIMEOUT_SECONDS")
        return self


class Settings(KitSettings):
    """Settings of the standalone HTTP service."""

    # Authentication
    auth_mode: Literal["entra", "development"] = "entra"
    entra_tenant_id: str | None = None
    entra_audience: Annotated[list[str], NoDecode] = Field(default_factory=list)
    entra_authority_host: str = "https://login.microsoftonline.com"
    invoke_role: str = "Pool.Invoke"
    admin_role: str = "Pool.Admin"
    diagnostics_role: str = "Pool.Diagnostics"
    # An app-only token identifies a service, not a person, so every end user behind the same
    # middle tier would share one stateful session. "reject" refuses app-only callers on stateful
    # agents unless they name the end user in X-Pool-Subject (which needs the delegate role).
    app_only_policy: Literal["reject", "allow"] = "reject"
    delegate_role: str = "Pool.Delegate"
    jwks_cache_seconds: int = Field(default=3600, ge=1)
    jwt_leeway_seconds: int = Field(default=60, ge=0)

    # Request limits
    max_body_bytes: int = Field(default=1_048_576, ge=1)

    # Operations
    readiness_probe_foundry: bool = False
    metrics_endpoint_enabled: bool = False
    diagnostic_session_id_header: bool = False
    host: str = "127.0.0.1"  # the container image sets POOL_HOST=0.0.0.0
    port: int = Field(default=8080, ge=1, le=65535)

    @field_validator("entra_audience", mode="before")
    @classmethod
    def _split_audience(cls, value: object) -> object:
        if isinstance(value, str):
            return [item.strip() for item in value.split(",") if item.strip()]
        return value

    @model_validator(mode="after")
    def _check_auth(self) -> Self:
        if self.auth_mode == "entra" and (not self.entra_tenant_id or not self.entra_audience):
            raise ConfigError(
                "POOL_AUTH_MODE=entra requires POOL_ENTRA_TENANT_ID and POOL_ENTRA_AUDIENCE"
            )
        return self
