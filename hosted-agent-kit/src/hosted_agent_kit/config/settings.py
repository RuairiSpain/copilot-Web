"""Settings read from the environment (never from YAML).

``KitSettings`` holds what the scheduling runtime needs and is what an application that embeds
the kit uses. ``Settings`` adds the options of the standalone HTTP service (caller
authentication, request limits, operations endpoints).
"""

from __future__ import annotations

from pathlib import Path
from typing import Annotated, Literal, Self

from pydantic import (
    AliasChoices,
    BaseModel,
    ConfigDict,
    Field,
    SecretStr,
    field_validator,
    model_validator,
)
from pydantic_settings import BaseSettings, NoDecode, SettingsConfigDict

from hosted_agent_kit.config.models import ConfigError

MIN_SESSION_KEY_CHARS = 32


class _Section(BaseModel):
    model_config = ConfigDict(extra="forbid")


class LedgerSettings(_Section):
    """Optional shared store for the regional view (borrowing and visibility). See quota.md."""

    backend: Literal["memory", "redis"] = "redis"
    url: SecretStr | None = Field(
        default=None, description="redis:// or rediss:// URL. Set it in the environment."
    )
    entra_auth: bool = Field(
        default=False, description="Authenticate to Azure Managed Redis with the kit's identity."
    )
    ttl_seconds: float = Field(
        default=30, gt=0, description="Entries expire this long after a heartbeat."
    )
    timeout_seconds: float = Field(
        default=0.25, gt=0, description="Per-call limit; slower counts as down."
    )


class QuotaSettings(_Section):
    """Active-session budget for one kit. Nothing is enforced unless a budget is set."""

    budget: int | None = Field(
        default=None,
        ge=1,
        description="Most compute-holding sessions this kit may use, all agents.",
    )
    min_limit: int = Field(default=1, ge=1, description="The adaptive limit never goes below this.")
    decrease_factor: float = Field(default=0.7, gt=0, lt=1)
    increase_step: int = Field(default=1, ge=1)
    probe_seconds: float = Field(
        default=60, gt=0, description="Raise the limit one step after this long without a refusal."
    )
    cooldown_seconds: float = Field(
        default=30, ge=0, description="After a decrease, ignore further refusals for this long."
    )
    subscription_id: str | None = None
    region: str | None = None
    region_limit: int | None = Field(
        default=None,
        ge=1,
        description="The subscription's regional limit, for reporting and borrowing.",
    )
    spare: int = Field(
        default=0,
        ge=0,
        description="Sessions kept unallocated for any kit to borrow (needs the ledger).",
    )
    ledger: LedgerSettings | None = None

    @model_validator(mode="after")
    def _check(self) -> Self:
        if self.ledger is not None and (self.region_limit is None or not self.region):
            raise ConfigError("quota.ledger needs quota.region and quota.region_limit")
        if self.spare and self.ledger is None:
            raise ConfigError("quota.spare needs quota.ledger")
        if self.budget is not None and self.min_limit > self.budget:
            raise ConfigError("quota.min_limit cannot exceed quota.budget")
        return self


class ShardSettings(_Section):
    index: int = Field(ge=0)
    count: int = Field(ge=1)

    @model_validator(mode="after")
    def _check(self) -> Self:
        if self.index >= self.count:
            raise ConfigError("shard.index must be less than shard.count")
        return self


class OwnershipSettings(_Section):
    """How a kit proves it is the only one scheduling an agent (or shard of an agent)."""

    backend: Literal["none", "memory", "redis"] = "none"
    url: SecretStr | None = None
    ttl_seconds: float = Field(default=30, gt=0)
    renew_seconds: float = Field(default=10, gt=0)
    quiet_seconds: float = Field(
        default=30, ge=0, description="After taking over, wait this long before admitting calls."
    )
    standby: bool = Field(
        default=False,
        description="If another kit owns the agent, wait as a standby; else refuse to start.",
    )

    @model_validator(mode="after")
    def _check(self) -> Self:
        if self.renew_seconds * 2 >= self.ttl_seconds:
            raise ConfigError("ownership.renew_seconds must be under half of ownership.ttl_seconds")
        if self.backend == "redis" and self.url is None:
            raise ConfigError("ownership.backend redis needs ownership.url")
        return self


class KitSettings(BaseSettings):
    """Runtime settings shared by the embedded kit and the standalone service."""

    model_config = SettingsConfigDict(
        env_prefix="POOL_", env_nested_delimiter="__", extra="ignore", populate_by_name=True
    )

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

    user_isolation_secret: SecretStr | None = Field(
        default=None,
        description="Secret that derives each user's x-ms-user-isolation-key. Needed when an "
        "agent sets user_isolation. Defaults to session_id_key. Keep it stable.",
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

    # How the kit counts sessions against the quota (see quota.md).
    idle_status_deprovisions: bool = Field(
        default=False,
        description="Treat a session that Foundry reports as idle as not holding compute.",
    )
    evict_idle_for_quota: bool = Field(
        default=True,
        description="Stop the least recently used idle session when an active limit is reached.",
    )
    quota_tick_seconds: float = Field(
        default=5, gt=0, description="How often the kit re-checks quota and serves waiting callers."
    )
    quota: QuotaSettings = Field(default_factory=QuotaSettings)

    # Which agents this kit schedules, and how it proves it is the only one doing so.
    kit_id: str | None = Field(default=None, pattern=r"^[A-Za-z0-9._-]{1,64}$")
    owns: Annotated[list[str] | None, NoDecode] = Field(
        default=None, description="Agents this kit schedules. Unset: every configured agent."
    )
    shard: ShardSettings | None = None
    ownership: OwnershipSettings = Field(default_factory=OwnershipSettings)

    # Largest request body the SDK accepts (the standalone service has its own limit).
    max_request_bytes: int | None = Field(default=None, ge=1)

    log_level: str = "INFO"
    service_name: str = "hosted-agent-kit"

    @field_validator("owns", mode="before")
    @classmethod
    def _split_owns(cls, value: object) -> object:
        if isinstance(value, str):
            return [item.strip() for item in value.split(",") if item.strip()]
        return value

    @model_validator(mode="after")
    def _check_session_key(self) -> Self:
        key = self.session_id_key
        if key is not None and len(key.get_secret_value()) < MIN_SESSION_KEY_CHARS:
            raise ConfigError(
                f"POOL_SESSION_ID_KEY must be at least {MIN_SESSION_KEY_CHARS} characters"
            )
        return self

    @model_validator(mode="after")
    def _check_isolation_secret(self) -> Self:
        secret = self.user_isolation_secret
        if secret is not None and len(secret.get_secret_value()) < MIN_SESSION_KEY_CHARS:
            raise ConfigError(
                f"POOL_USER_ISOLATION_SECRET must be at least {MIN_SESSION_KEY_CHARS} characters"
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
