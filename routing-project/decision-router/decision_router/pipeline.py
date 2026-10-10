"""Stage 1 filter -> Decision-1 ranking -> decision log -> call the selected model.

Stage 1 (stage1.py) narrows the chosen catalog (`compatibility`: old or new) by lifecycle, the
operator allow-list, the request's routing_constraints, what the request itself needs (tools,
images, JSON schema, streaming, size), location and the routing mode's price band.

The model called first is the one with the highest probability, unless that probability is
below ROUTER_LOW_CONFIDENCE_THRESHOLD; then the second-ranked model is called first and the
top model second. If a call fails, the next model in that order is tried. Every
model tried is inside the stage-1 set, so the routing mode is never exceeded. If
Decision-1 itself fails, the request fails: there is no other router behind it.
"""
from __future__ import annotations

import asyncio
import random
import time
from collections.abc import AsyncIterator, Awaitable, Callable
from dataclasses import dataclass, field
from typing import Any

import httpx

from .anthropic import chat_usage as anthropic_usage
from .catalog import Catalog, CatalogError, Catalogs, ModelEntry
from .config import ROUTING_MODES, Settings
from .decision1 import Decision, Decision1Client, DecisionError, build_state, execution_order
from .foundry import ChatClient
from .pricing import PriceTable
from .stage1 import ConstraintError, Constraints, parse_constraints, select
from .telemetry import Telemetry

# Extension fields this router reads; removed before the body is forwarded to a model.
ROUTER_FIELDS = ("routing_mode", "compatibility", "routing_constraints", "claude_translation")


class RoutingError(Exception):
    """A failure this service reports in the OpenAI error shape."""

    def __init__(self, status_code: int, code: str, message: str, attempts: list[dict[str, Any]] | None = None,
                 details: dict[str, Any] | None = None):
        super().__init__(message)
        self.status_code = status_code
        self.code = code
        self.attempts = attempts or []
        self.details = details or {}

    def body(self) -> dict[str, Any]:
        error: dict[str, Any] = {"message": str(self), "type": "router_error", "code": self.code}
        if self.attempts:
            error["attempts"] = self.attempts
        error.update(self.details)
        return {"error": error}


class ProviderError(Exception):
    """A non-retryable error from the model (bad request, content filter, auth). Passed through unchanged."""

    def __init__(self, status_code: int, content: bytes, content_type: str | None, attempts: list[dict[str, Any]]):
        super().__init__(f"provider returned HTTP {status_code}")
        self.status_code = status_code
        self.content = content
        self.content_type = content_type or "application/json"
        self.attempts = attempts


def _retryable(status: int) -> bool:
    return status in (408, 429) or status >= 500


def _retry_after_seconds(response: httpx.Response) -> float | None:
    ms = response.headers.get("retry-after-ms")
    if ms:
        try:
            return float(ms) / 1000
        except ValueError:
            pass
    seconds = response.headers.get("retry-after")
    if seconds:
        try:
            return float(seconds)
        except ValueError:
            return None  # HTTP-date form: fall back to backoff
    return None


@dataclass
class Prepared:
    """A validated request: which catalog, which mode, which constraints, and the body to forward."""
    mode: str
    catalog: Catalog
    constraints: Constraints
    body: dict[str, Any]
    # claude_translation=false: `body` is an Anthropic Messages request, forwarded to Claude unchanged.
    passthrough: bool = False


@dataclass
class Routed:
    request_id: str
    mode: str
    catalog: Catalog
    candidates: list[ModelEntry]
    decision: Decision
    started: float
    event: dict[str, Any] = field(default_factory=dict)
    # Models in the order they will be called (see execution_order).
    order: list[str] = field(default_factory=list)
    low_confidence: bool = False
    passthrough: bool = False


@dataclass
class Completion:
    routed: Routed
    served: ModelEntry
    status_code: int
    content: bytes
    content_type: str
    attempts: list[dict[str, Any]]


@dataclass
class StreamingCompletion:
    routed: Routed
    served: ModelEntry
    status_code: int
    content_type: str
    attempts: list[dict[str, Any]]
    chunks: AsyncIterator[bytes]


class RouterPipeline:
    def __init__(self, settings: Settings, catalogs: Catalogs, decision: Decision1Client, chat: ChatClient,
                 telemetry: Telemetry, prices: PriceTable, *,
                 sleep: Callable[[float], Awaitable[None]] = asyncio.sleep,
                 clock: Callable[[], float] = time.monotonic):
        self.settings = settings
        self.catalogs = catalogs
        self.decision = decision
        self.chat = chat
        self.telemetry = telemetry
        self.prices = prices
        self._sleep = sleep
        self._clock = clock

    # ------------------------------------------------------------------ request shape
    def prepare(self, body: Any) -> Prepared:
        """Validate a Model-Router-style body plus this router's optional extension fields."""
        if not isinstance(body, dict):
            raise RoutingError(400, "invalid_request", "request body must be a JSON object")
        messages = body.get("messages")
        if not isinstance(messages, list) or not messages or not all(isinstance(m, dict) for m in messages):
            raise RoutingError(400, "invalid_request", "'messages' must be a non-empty array of message objects")
        try:
            catalog = self.catalogs.get(body.get("compatibility"))
        except CatalogError as exc:
            raise RoutingError(400, "invalid_compatibility", str(exc)) from exc
        mode = body.get("routing_mode") or catalog.default_mode
        if mode not in ROUTING_MODES:
            raise RoutingError(400, "invalid_routing_mode", f"routing_mode must be one of {list(ROUTING_MODES)}")
        translation = body.get("claude_translation", True)
        if not isinstance(translation, bool):
            raise RoutingError(400, "invalid_request", "claude_translation must be true or false")
        try:
            constraints = parse_constraints(body.get("routing_constraints"), catalog)
        except ConstraintError as exc:
            raise RoutingError(400, "invalid_routing_constraints", str(exc)) from exc
        # `model` names the router deployment the client called; it is replaced per target.
        forwarded = {k: v for k, v in body.items() if k not in ROUTER_FIELDS and k != "model"}
        return Prepared(mode, catalog, constraints, forwarded, passthrough=not translation)

    # ------------------------------------------------------------------ stages 1 and 2
    async def route(self, prepared: Prepared, request_id: str) -> Routed:
        started = self._clock()
        catalog, mode, body = prepared.catalog, prepared.mode, prepared.body
        stage1 = select(catalog, mode, prepared.constraints, body, self.settings, passthrough=prepared.passthrough)
        candidates = stage1.candidates
        event: dict[str, Any] = {
            "request_id": request_id,
            "routing_mode": mode,
            "compatibility": catalog.compatibility,
            "catalog_sha256": catalog.fingerprint,
            "routing_constraints": prepared.constraints.as_dict(),
            "claude_translation": not prepared.passthrough,
            "stage1": stage1.log(),
            "candidates": [m.name for m in candidates],
            "stream": bool(body.get("stream")),
        }
        if not candidates:
            event.update(outcome="no_eligible_models", total_latency_ms=round((self._clock() - started) * 1000, 3))
            self.telemetry.record(event, body)
            raise RoutingError(422, "no_eligible_models",
                               f"no model in the '{catalog.compatibility}' catalog passes stage 1; the "
                               f"'{stage1.empty_at}' step removed the last candidates",
                               details={"stage1": stage1.log()})
        try:
            decision = await self.decision.decide(
                build_state(body, self.settings.state_max_chars),
                catalog.modes[mode].instructions,
                catalog.criteria(candidates),
                candidates,
            )
        except DecisionError as exc:
            event.update(outcome="decision_failed", error_code=exc.code,
                         total_latency_ms=round((self._clock() - started) * 1000, 3))
            self.telemetry.record(event, body)
            status = 504 if exc.code == "timeout" else 503 if exc.status_code == 429 else 502
            raise RoutingError(status, "decision_unavailable", f"Decision-1 failed: {exc}") from exc
        order, low_confidence = execution_order(decision.ranking, decision.probabilities,
                                                0.0 if decision.skipped else self.settings.low_confidence_threshold)
        event["decision"] = {
            "deployment": self.decision.deployment,
            "ranking": decision.ranking,
            "execution_order": order,
            "top_probability": decision.probabilities[decision.ranking[0]],
            "low_confidence": low_confidence,
            "low_confidence_threshold": self.settings.low_confidence_threshold,
            "probabilities": decision.probabilities,
            "choice": decision.choice,
            "confidence": decision.confidence,
            "latency_ms": decision.latency_ms,
            "skipped": decision.skipped,
            "attempts": decision.attempts,
            "response_model": decision.response_model,
            "usage": decision.usage,
            "cost": self.prices.decision_cost(decision.usage),
        }
        return Routed(request_id, mode, catalog, candidates, decision, started, event, order, low_confidence,
                      prepared.passthrough)

    def record_route_only(self, routed: Routed, body: dict[str, Any]) -> None:
        routed.event.update(outcome="routed", total_latency_ms=round((self._clock() - routed.started) * 1000, 3))
        self.telemetry.record(routed.event, body)

    # ------------------------------------------------------------------ execution
    async def _call_ranked(self, routed: Routed, body: dict[str, Any], stream: bool
                           ) -> tuple[ModelEntry, httpx.Response, list[dict[str, Any]]]:
        settings = self.settings
        deadline = routed.started + settings.total_timeout_seconds
        attempts: list[dict[str, Any]] = []
        for name in routed.order:
            entry = routed.catalog.by_name[name]
            for attempt in range(1, settings.attempts_per_model + 1):
                remaining = deadline - self._clock()
                if remaining <= 0:
                    raise RoutingError(504, "deadline_exceeded", "no model answered within the time budget", attempts)
                t0 = self._clock()
                record: dict[str, Any] = {"model": name, "attempt": attempt, "status": None}
                try:
                    response = await self.chat.send(entry, body, passthrough=routed.passthrough,
                                                    timeout=min(settings.request_timeout_seconds, remaining),
                                                    stream=stream)
                except httpx.TimeoutException:
                    response, record["outcome"] = None, "timeout"
                except httpx.HTTPError as exc:
                    response, record["outcome"] = None, f"network_error:{type(exc).__name__}"
                record["latency_ms"] = round((self._clock() - t0) * 1000, 3)
                retry_after = None
                if response is not None:
                    record["status"] = response.status_code
                    if response.status_code < 400:
                        record["outcome"] = "success"
                        attempts.append(record)
                        return entry, response, attempts
                    retry_after = _retry_after_seconds(response)
                    if response.status_code == 404:
                        # Deployment missing: retrying the same model cannot help.
                        record["outcome"] = "not_found"
                        attempts.append(record)
                        await response.aclose()
                        break
                    if not _retryable(response.status_code):
                        content = await response.aread()
                        await response.aclose()
                        record["outcome"] = "client_error"
                        attempts.append(record)
                        raise ProviderError(response.status_code, content, response.headers.get("content-type"),
                                            attempts)
                    record["outcome"] = "retryable_error"
                    await response.aclose()
                attempts.append(record)
                if attempt == settings.attempts_per_model:
                    break
                if retry_after is not None and retry_after > settings.max_retry_after_seconds:
                    break  # the deployment asked for a longer pause than we allow: try the next model
                delay = retry_after if retry_after is not None else 0.5 * 2 ** (attempt - 1) * (0.75 + random.random() / 2)
                await self._sleep(min(delay, max(0.0, deadline - self._clock())))
        raise RoutingError(502, "all_models_failed", "every ranked model failed", attempts)

    def _finish(self, routed: Routed, body: dict[str, Any], served: ModelEntry | None,
                attempts: list[dict[str, Any]], outcome: str, response_json: Any = None,
                error_code: str | None = None) -> None:
        usage = response_json.get("usage") if isinstance(response_json, dict) else None
        if isinstance(usage, dict) and "input_tokens" in usage and "prompt_tokens" not in usage:
            usage = anthropic_usage(usage)  # native Claude response (claude_translation=false)
        provider_model = response_json.get("model") if isinstance(response_json, dict) else None
        routed.event.update(
            outcome=outcome,
            served_model=served.name if served else None,
            served_deployment=served.deployment if served else None,
            provider_model=provider_model,
            fallback_used=bool(served and served.name != routed.order[0]),
            attempts=attempts,
            usage=usage,
            cost=self.prices.cost(served.name, usage) if served else None,
            total_latency_ms=round((self._clock() - routed.started) * 1000, 3),
        )
        if error_code:
            routed.event["error_code"] = error_code
        self.telemetry.record(routed.event, body)

    async def complete(self, routed: Routed, body: dict[str, Any]) -> Completion:
        try:
            served, response, attempts = await self._call_ranked(routed, body, stream=False)
        except ProviderError as exc:
            self._finish(routed, body, None, exc.attempts, "provider_error", error_code=str(exc.status_code))
            raise
        except RoutingError as exc:
            self._finish(routed, body, None, exc.attempts, "failed", error_code=exc.code)
            raise
        content = response.content
        try:
            parsed = response.json()
        except ValueError:
            parsed = None
        self._finish(routed, body, served, attempts, "success", parsed)
        return Completion(routed, served, response.status_code, content,
                          response.headers.get("content-type", "application/json"), attempts)

    async def stream(self, routed: Routed, body: dict[str, Any]) -> StreamingCompletion:
        """Fallback is possible only until the first byte; after that the stream belongs to one model."""
        try:
            served, response, attempts = await self._call_ranked(routed, body, stream=True)
        except ProviderError as exc:
            self._finish(routed, body, None, exc.attempts, "provider_error", error_code=str(exc.status_code))
            raise
        except RoutingError as exc:
            self._finish(routed, body, None, exc.attempts, "failed", error_code=exc.code)
            raise

        async def chunks() -> AsyncIterator[bytes]:
            outcome = "success"
            try:
                async for chunk in response.aiter_raw():
                    yield chunk
            except BaseException:  # includes the client disconnecting (GeneratorExit / cancellation)
                outcome = "stream_interrupted"
                raise
            finally:
                await response.aclose()
                self._finish(routed, body, served, attempts, outcome)

        return StreamingCompletion(routed, served, response.status_code,
                                   response.headers.get("content-type", "text/event-stream"), attempts, chunks())

    async def close(self) -> None:
        await self.decision.close()
        await self.chat.close()
        await self.decision.auth.close()
        if self.chat.auth is not self.decision.auth:
            await self.chat.auth.close()
        self.telemetry.close()
