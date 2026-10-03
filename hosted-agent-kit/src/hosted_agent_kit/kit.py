"""``Hack``: the object a developer creates once and calls from their own endpoints."""

from __future__ import annotations

import asyncio
import json
import logging
import os
import time
import uuid
from collections.abc import Callable, Mapping
from pathlib import Path
from types import TracebackType
from typing import Any

from hosted_agent_kit.administration import Administration
from hosted_agent_kit.config.loader import (
    build_config,
    build_settings_overrides,
    load_document,
    parse_yaml,
)
from hosted_agent_kit.config.models import AgentConfig, AgentPoolConfig, ConfigError
from hosted_agent_kit.config.settings import KitSettings
from hosted_agent_kit.domain.enums import AgentProtocol
from hosted_agent_kit.domain.errors import (
    IdempotencyInProgressError,
    IdempotencyKeyReusedError,
    NotActiveError,
    ValidationFailedError,
    WrongShardError,
)
from hosted_agent_kit.domain.identity import is_valid_conversation_key, is_valid_user_id
from hosted_agent_kit.logging_config import correlation_id_var, log_event
from hosted_agent_kit.ports.foundry import FoundryAdapter
from hosted_agent_kit.ports.ownership import OwnershipStore
from hosted_agent_kit.ports.quota import QuotaLedger
from hosted_agent_kit.reporting import Reporting
from hosted_agent_kit.results import AgentResult
from hosted_agent_kit.runtime import (
    Runtime,
    build_adapter,
    build_ownership_store,
    build_runtime,
    owned_config,
    project_id,
)
from hosted_agent_kit.services.clock import Clock, SystemClock
from hosted_agent_kit.services.idempotency import (
    IdempotencyStore,
    Outcome,
    StoredResponse,
    fingerprint,
)
from hosted_agent_kit.services.ownership import OwnershipManager, Role, lease_key
from hosted_agent_kit.services.pool import PoolRequest
from hosted_agent_kit.services.scheduling import PluginRegistry
from hosted_agent_kit.services.sharding import shard_for
from hosted_agent_kit.views import AgentInfo

logger = logging.getLogger(__name__)

Payload = Mapping[str, Any]


class Hack:
    """The Hosted Agent Controller Kit scheduler, embedded in your application.

    Create it from a scheduler YAML file, start it with your application, and call agents from
    your own endpoints::

        kit = Hack.from_yaml("scheduler.yaml")

        async with kit:
            result = await kit.responses("support-bot", user_id="u1", input={"input": "hi"})
            print(result.json())

    ``responses`` and ``invocations`` do what the pool does for every call: pick or create a
    session for the user, queue when the pool is full, retry throttling, and release the session
    afterwards. ``reporting`` and ``admin`` are read-only and administrative views of the same
    pool.
    """

    def __init__(
        self,
        config: AgentPoolConfig,
        *,
        settings: KitSettings | None = None,
        adapter: FoundryAdapter | None = None,
        clock: Clock | None = None,
        scheduler_plugins: PluginRegistry | None = None,
        config_loader: Callable[[], AgentPoolConfig] | None = None,
        ownership_store: OwnershipStore | None = None,
        ledger: QuotaLedger | None = None,
    ) -> None:
        self.settings = settings or KitSettings()
        self._ownership_store = ownership_store
        self._ledger = ledger
        kit_name = self.settings.kit_id or "kit"
        self.instance_id = f"{kit_name}-{uuid.uuid4().hex[:8]}"
        self._manager: OwnershipManager | None = None
        self._config = config
        self._clock = clock or SystemClock()
        self._adapter = adapter
        self._plugins = scheduler_plugins
        self._config_loader = config_loader
        self._runtime: Runtime | None = None
        self._idempotency: IdempotencyStore | None = None

    # ------------------------------------------------------------------ creation

    @classmethod
    def from_yaml(
        cls,
        path: str | Path,
        *,
        settings: KitSettings | None = None,
        adapter: FoundryAdapter | None = None,
        clock: Clock | None = None,
        scheduler_plugins: PluginRegistry | None = None,
        ownership_store: OwnershipStore | None = None,
        ledger: QuotaLedger | None = None,
    ) -> Hack:
        """Load ``agentPool`` (and an optional ``hack`` section) from a YAML file.

        The file may be a standalone scheduler file or an ``azure.yaml`` that has an
        ``agentPool`` section. Environment variables (``POOL_*``) override the ``hack`` section.
        The file is re-read by ``admin.reload_config()``.
        """
        file = Path(path)
        pool_config, overrides = load_document(file)
        return cls(
            pool_config,
            settings=settings or _settings_with(overrides),
            adapter=adapter,
            clock=clock,
            scheduler_plugins=scheduler_plugins,
            config_loader=lambda: load_document(file)[0],
            ownership_store=ownership_store,
            ledger=ledger,
        )

    @classmethod
    def from_dict(
        cls,
        document: Mapping[str, Any],
        *,
        settings: KitSettings | None = None,
        adapter: FoundryAdapter | None = None,
        clock: Clock | None = None,
        scheduler_plugins: PluginRegistry | None = None,
        ownership_store: OwnershipStore | None = None,
        ledger: QuotaLedger | None = None,
    ) -> Hack:
        """Build from an already parsed document: ``{"agentPool": {...}, "hack": {...}}``."""
        raw = dict(document)
        return cls(
            build_config(raw),
            settings=settings or _settings_with(build_settings_overrides(raw)),
            adapter=adapter,
            clock=clock,
            scheduler_plugins=scheduler_plugins,
            ownership_store=ownership_store,
            ledger=ledger,
        )

    @classmethod
    def from_yaml_text(cls, text: str, **kwargs: Any) -> Hack:
        """Build from YAML held in a string (useful in tests)."""
        return cls.from_dict(parse_yaml(text, "yaml text"), **kwargs)

    # ----------------------------------------------------------------- lifecycle

    async def start(self) -> None:
        """Connect to Foundry, run the first sync and start the controllers.

        With ``ownership`` configured, the kit first claims its agents. If another kit owns them,
        start fails with ``OwnershipConflictError``, or, with ``ownership.standby``, the kit waits
        as a standby and takes over when the owner stops renewing.
        """
        if self._manager is not None or (self._runtime is not None and self._runtime.started):
            return
        if self.settings.ownership.backend == "none":
            await self._activate()
            return
        store = self._ownership_store or build_ownership_store(self.settings, self._clock)
        shard = self.settings.shard.index if self.settings.shard is not None else None
        pid = project_id(self.settings)
        keys = [
            lease_key(pid, name, shard) for name in owned_config(self.settings, self._config).names
        ]
        self._manager = OwnershipManager(
            store,
            self.settings.ownership,
            instance_id=self.instance_id,
            keys=keys,
            clock=self._clock,
            on_activate=self._activate,
            on_deactivate=self._deactivate,
        )
        try:
            await self._manager.start()
        except BaseException:
            self._manager = None
            raise

    async def _activate(self) -> None:
        adapter = self._adapter or build_adapter(self.settings)
        self._runtime = build_runtime(
            self.settings,
            self._config,
            adapter,
            self._clock,
            self._config_loader,
            self._plugins,
            self._ledger,
        )
        self._idempotency = IdempotencyStore(
            self._clock,
            ttl_seconds=self.settings.idempotency_ttl_seconds,
            max_entries=self.settings.idempotency_max_entries,
            max_body_bytes=self.settings.idempotency_max_body_bytes,
        )
        try:
            await self._runtime.start()
            await self._wait_for_initial_sync()
        except BaseException:
            await self._runtime.stop()
            self._runtime = None
            raise

    async def _deactivate(self) -> None:
        runtime, self._runtime = self._runtime, None
        if runtime is not None:
            await runtime.stop()

    async def _wait_for_initial_sync(self) -> None:
        """Wait until Foundry's existing sessions are known, so a first call can reuse them."""
        assert self._runtime is not None
        reconciler = self._runtime.reconciler
        deadline = time.monotonic() + self.settings.startup_sync_timeout_seconds
        while not reconciler.initial_sync_done:
            if time.monotonic() >= deadline:
                log_event(
                    logger,
                    "initial_sync_incomplete",
                    level=logging.WARNING,
                    hint="Foundry may be unreachable; calls will create sessions as needed",
                )
                return
            await asyncio.sleep(0.01)

    async def stop(self) -> None:
        """Stop admitting calls, wait for running ones (``shutdown_grace_seconds``), then stop.

        An owning kit also releases its leases, so a standby can take over without waiting.
        """
        manager, self._manager = self._manager, None
        if manager is not None:
            await manager.stop()
        elif self._runtime is not None:
            await self._runtime.stop()

    async def __aenter__(self) -> Hack:
        await self.start()
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        await self.stop()

    @property
    def runtime(self) -> Runtime:
        if self._manager is not None and self._manager.role is not Role.ACTIVE:
            raise NotActiveError(
                "This kit is not the active owner of its agents. Another kit is serving them.",
                retry_after_seconds=self.settings.retry_after_seconds,
            )
        if self._runtime is None:
            raise RuntimeError("Hack is not started: use `async with kit:` or `await kit.start()`")
        return self._runtime

    @property
    def role(self) -> str:
        """``active`` (serving), ``standby`` (waiting to take over) or ``stopped``."""
        if self._manager is not None:
            return self._manager.role.value
        started = self._runtime is not None and self._runtime.started
        return Role.ACTIVE.value if started else Role.STOPPED.value

    @property
    def ready(self) -> bool:
        active = self._manager is None or self._manager.role is Role.ACTIVE
        return active and self._runtime is not None and self._runtime.ready

    @property
    def ownership(self) -> OwnershipManager | None:
        return self._manager

    def shard_for(self, user_id: str) -> int | None:
        """The shard that serves ``user_id`` (None when this kit is not sharded)."""
        shard = self.settings.shard
        return shard_for(user_id, shard.count) if shard is not None else None

    @property
    def config(self) -> AgentPoolConfig:
        return self._runtime.config.current if self._runtime is not None else self._config

    @property
    def agent_names(self) -> list[str]:
        return list(self.config.names)

    @property
    def reporting(self) -> Reporting:
        """Read-only views: pools, sessions, events, metrics, health."""
        return Reporting(self.runtime)

    @property
    def admin(self) -> Administration:
        """Administrative actions: delete a session, force a sync, reload, add capacity."""
        return Administration(self.runtime)

    # ----------------------------------------------------------------- discovery

    def describe(self, agent: str) -> AgentInfo:
        cfg = self.runtime.pool.agent_config(agent)
        invocations = cfg.protocol is AgentProtocol.INVOCATIONS
        s = self.settings
        return AgentInfo(
            name=cfg.name,
            protocol=cfg.protocol.value,
            stateful=cfg.stateful,
            streaming="agent" if invocations else "request",
            supports_conversation_key=cfg.stateful,
            queue_enabled=cfg.queue.enabled,
            queue_max_wait_seconds=cfg.queue.max_wait_seconds if cfg.queue.enabled else None,
            agent_version=cfg.agent_version,
            default_timeout_seconds=s.default_timeout_seconds,
            max_timeout_seconds=s.max_timeout_seconds,
            max_stream_seconds=s.max_stream_seconds,
        )

    def list_agents(self) -> list[AgentInfo]:
        return [self.describe(name) for name in self.agent_names]

    # --------------------------------------------------------------------- calls

    async def responses(
        self,
        agent: str,
        *,
        user_id: str,
        input: Payload,
        stream: bool = False,
        conversation_key: str | None = None,
        timeout_seconds: float | None = None,
        idempotency_key: str | None = None,
    ) -> AgentResult:
        """Call a Responses-protocol agent.

        ``input`` is the Responses request body, for example ``{"input": "Hello"}``. With
        ``stream=True`` the result is a stream: iterate ``result.chunks()``.
        """
        cfg = self._agent(agent, AgentProtocol.RESPONSES)
        return await self._call(
            cfg,
            user_id=user_id,
            payload=dict(input),
            stream=stream,
            conversation_key=conversation_key,
            timeout_seconds=timeout_seconds,
            idempotency_key=idempotency_key,
            route="responses",
        )

    async def ask(
        self,
        agent: str,
        message: str,
        *,
        user_id: str,
        conversation_key: str | None = None,
        timeout_seconds: float | None = None,
        idempotency_key: str | None = None,
    ) -> AgentResult:
        """Send one text message to a Responses agent (shorthand for ``responses``)."""
        return await self.responses(
            agent,
            user_id=user_id,
            input={"input": message},
            conversation_key=conversation_key,
            timeout_seconds=timeout_seconds,
            idempotency_key=idempotency_key,
        )

    async def invocations(
        self,
        agent: str,
        *,
        user_id: str,
        body: bytes | str | Payload | None = None,
        content_type: str | None = None,
        conversation_key: str | None = None,
        timeout_seconds: float | None = None,
        idempotency_key: str | None = None,
    ) -> AgentResult:
        """Call an Invocations-protocol agent with any request body.

        A mapping is sent as JSON. ``bytes`` and ``str`` are sent as they are, with
        ``content_type`` (default ``application/octet-stream`` or ``text/plain``). The agent
        decides the status code, content type and whether to stream.
        """
        cfg = self._agent(agent, AgentProtocol.INVOCATIONS)
        raw, media = _encode_body(body, content_type)
        return await self._call(
            cfg,
            user_id=user_id,
            payload=None,
            stream=False,
            conversation_key=conversation_key,
            timeout_seconds=timeout_seconds,
            idempotency_key=idempotency_key,
            raw_body=raw,
            content_type=media,
            route="invocations",
        )

    # ------------------------------------------------------------------ internals

    def _agent(self, name: str, expected: AgentProtocol) -> AgentConfig:
        cfg = self.runtime.pool.agent_config(name)  # AgentNotConfiguredError otherwise
        if cfg.protocol is not expected:
            wanted = "responses" if cfg.protocol is AgentProtocol.RESPONSES else "invocations"
            raise ValidationFailedError(
                f"Agent '{name}' uses the {cfg.protocol.value} protocol. Call kit.{wanted}()."
            )
        return cfg

    async def _call(
        self,
        cfg: AgentConfig,
        *,
        user_id: str,
        payload: Any,
        stream: bool,
        conversation_key: str | None,
        timeout_seconds: float | None,
        idempotency_key: str | None,
        route: str,
        raw_body: bytes | None = None,
        content_type: str | None = None,
    ) -> AgentResult:
        if not is_valid_user_id(user_id):
            raise ValidationFailedError(
                "user_id must be 1-128 characters of letters, digits, '.', '_', ':' or '-'."
            )
        if conversation_key is not None:
            if not is_valid_conversation_key(conversation_key):
                raise ValidationFailedError("conversation_key has an invalid format.")
            if not cfg.stateful:
                raise ValidationFailedError("conversation_key only applies to stateful agents.")
        s = self.settings
        if s.shard is not None and cfg.stateful:
            owner = shard_for(user_id, s.shard.count)
            if owner != s.shard.index:
                raise WrongShardError(
                    f"This user is served by shard {owner} of {s.shard.count}, "
                    f"not {s.shard.index}.",
                    shard=owner,
                    count=s.shard.count,
                )
        timeout = min(timeout_seconds or s.default_timeout_seconds, s.max_timeout_seconds)
        store = self._idempotency
        scope: str | None = None
        if idempotency_key and store is not None and store.enabled and not stream:
            scope = f"{user_id}\x00{cfg.name}\x00{idempotency_key}"
            outcome, stored = store.begin(
                scope,
                fingerprint(
                    route,
                    raw_body if raw_body is not None else _canonical(payload),
                    content_type or "",
                    conversation_key or "",
                ),
            )
            if outcome is Outcome.MISMATCH:
                scope = None
                raise IdempotencyKeyReusedError(
                    "This idempotency_key was already used with a different request."
                )
            if outcome is Outcome.IN_PROGRESS:
                scope = None
                raise IdempotencyInProgressError(
                    "A call with this idempotency_key is still running.",
                    retry_after_seconds=s.retry_after_seconds,
                )
            if outcome is Outcome.REPLAY and stored is not None:
                return AgentResult(
                    request_id=uuid.uuid4().hex,
                    status_code=stored.status_code,
                    media_type=stored.media_type,
                    content=stored.body,
                    replayed=True,
                )
        request = PoolRequest(
            agent_name=cfg.name,
            user_id=user_id,
            payload=payload,
            timeout_seconds=timeout,
            request_id=uuid.uuid4().hex,
            correlation_id=correlation_id_var.get() or uuid.uuid4().hex,
            idempotency_key=idempotency_key,
            stream=stream,
            conversation_key=conversation_key,
            raw_body=raw_body,
            content_type=content_type,
        )
        try:
            pool_result = await self.runtime.pool.execute(request)
        except BaseException:
            if scope is not None and store is not None:
                store.abandon(scope)
            raise
        result = AgentResult.from_pool(pool_result)
        if scope is not None and store is not None:
            if result.is_stream:
                store.abandon(scope)  # a stream cannot be replayed
            else:
                store.complete(
                    scope, StoredResponse(result.status_code, result.media_type, result.content)
                )
        return result


def _env_names(field_name: str) -> set[str]:
    field = KitSettings.model_fields[field_name]
    names = {f"POOL_{field_name}".upper()}
    alias = field.validation_alias
    choices = getattr(alias, "choices", None)
    if isinstance(alias, str):
        names.add(alias.upper())
    elif choices:
        names.update(c.upper() for c in choices if isinstance(c, str))
    return names


def _settings_with(overrides: dict[str, Any]) -> KitSettings:
    """``KitSettings`` from the environment, with ``hack`` file values for what it leaves unset.

    Environment variables win over the file. The merged values are validated once, so a file that
    sets only one of two related limits is judged together with the environment.
    """
    env = {name.upper() for name in os.environ}
    from_file = {key: value for key, value in overrides.items() if not (_env_names(key) & env)}
    try:
        return KitSettings(**from_file)
    except ConfigError:
        raise
    except ValueError as exc:
        raise ConfigError(f"invalid hack settings: {exc}") from exc


def _canonical(payload: Any) -> str:
    return json.dumps(payload, sort_keys=True, separators=(",", ":"), default=str)


def _encode_body(
    body: bytes | str | Payload | None, content_type: str | None
) -> tuple[bytes, str | None]:
    if body is None:
        return b"", content_type
    if isinstance(body, bytes):
        return body, content_type or "application/octet-stream"
    if isinstance(body, str):
        return body.encode(), content_type or "text/plain; charset=utf-8"
    return json.dumps(body).encode(), content_type or "application/json"
