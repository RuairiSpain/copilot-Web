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
import json
import random
import time
from collections.abc import AsyncIterator, Awaitable, Callable
from dataclasses import dataclass, field, replace
from typing import Any

import httpx

from .anthropic import chat_usage as anthropic_usage
from .catalog import Catalog, CatalogError, Catalogs, CostBand, ModelEntry
from .config import ROUTING_MODES, Settings
from .decision1 import Decision, Decision1Client, DecisionError, build_state, execution_order
from .foundry import ChatClient
from .instructions import InstructionTemplates
from .pinning import build_pin_store, conversation_key
from .policy import CallerPolicy, PolicyError, load_policies, policy_for
from .pricing import PriceTable
from .stage1 import ConstraintError, Constraints, parse_constraints, parse_cost_band, select
from .telemetry import Telemetry

# Extension fields this router reads; removed before the body is forwarded to a model.
ROUTER_FIELDS = ("routing_mode", "compatibility", "routing_constraints", "claude_translation", "routing_pin",
                 "routing_cost_band")


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


# APIs whose streams can end with a usage chunk (stream_options.include_usage). MAI is left out:
# its chat endpoint is not documented to accept stream_options.
USAGE_STREAM_APIS = ("openai_chat", "anthropic_messages")


class UsageTap:
    """Reads token usage out of a server-sent event stream as it passes through.

    Chat-completions streams: the usage-only chunk (`choices: []` and `usage`) is recorded, and dropped
    from the output unless the caller asked for it. Native Anthropic streams (claude_translation=false):
    usage from `message_start` and `message_delta` is merged; nothing is removed."""

    def __init__(self, keep_usage_chunk: bool):
        self.keep_usage_chunk = keep_usage_chunk
        self.usage: dict[str, Any] | None = None
        self._buffer = b""

    def feed(self, chunk: bytes) -> bytes:
        self._buffer += chunk
        out = []
        while True:
            end, size = min(((self._buffer.find(sep), len(sep)) for sep in (b"\n\n", b"\r\n\r\n")
                             if self._buffer.find(sep) >= 0), default=(-1, 0))
            if end < 0:
                break
            event, self._buffer = self._buffer[:end + size], self._buffer[end + size:]
            if self._keep(event):
                out.append(event)
        return b"".join(out)

    def finish(self) -> bytes:
        rest, self._buffer = self._buffer, b""
        return rest

    def _keep(self, event: bytes) -> bool:
        for line in event.splitlines():
            if not line.startswith(b"data:"):
                continue
            try:
                data = json.loads(line[5:].strip() or b"null")
            except ValueError:
                return True
            if not isinstance(data, dict):
                return True
            usage = data.get("usage") or (data.get("message") or {}).get("usage")
            if isinstance(usage, dict):
                self.usage = {**(self.usage or {}), **usage}
                if data.get("choices") == [] and not self.keep_usage_chunk:
                    return False
        return True


# Request parameters a model may refuse without the request being wrong: the router drops them and
# retries the same model once, so callers can always send them (stream_options is added by the router).
DROPPABLE_PARAMS = ("temperature", "top_p", "reasoning_effort", "stream_options")


def _rejected_params(content: bytes, body: dict[str, Any]) -> list[str]:
    """The droppable parameters in `body` that a 400 error message names."""
    text = content.decode("utf-8", "replace").lower()
    return [p for p in DROPPABLE_PARAMS if p in body and p in text]


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
    profile: str | None = None
    caller: dict[str, Any] | None = None  # who called (inbound.Caller.log()), for the decision log
    pin: bool = False  # routing_pin: keep the conversation on one model (pinning.py)
    session_id: str | None = None  # x-router-session-id header, when sent
    cost_band: CostBand | None = None  # routing_cost_band: replaces the mode's price band in stage 1
    policy: CallerPolicy | None = None  # the operator's limits for this caller (policy.py)


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
    pin_key: str | None = None  # set when pinning is on: a successful answer (re)pins the conversation
    instructions: str = ""  # what Decision-1 was (or would have been) asked, from the instruction templates
    criteria: dict[str, str] = field(default_factory=dict)


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
        self.pins = build_pin_store(settings.pin_store_url, settings.pin_ttl_seconds, settings.pin_max_entries, clock)
        self.templates = InstructionTemplates.load(settings.instructions_path)
        self._check_profiles()
        try:
            self.policies = load_policies(settings.caller_policies)
        except PolicyError as exc:
            raise ValueError(f"ROUTER_CALLER_POLICIES: {exc}") from exc
        known = {m.name for c in catalogs.catalogs.values() for m in c.models}
        for policy in self.policies.values():
            if set(policy.models) - known:
                raise ValueError(f"ROUTER_CALLER_POLICIES '{policy.name}' names unknown models "
                                 f"{sorted(set(policy.models) - known)}")

    def _check_profiles(self) -> None:
        """Fail at startup, not on every request, when a profile names a model, band or mode the catalog lacks."""
        for name, profile in self.settings.profiles.items():
            try:
                catalog = self.catalogs.get(profile.get("compatibility"))
                if profile.get("routing_mode", catalog.default_mode) not in ROUTING_MODES:
                    raise ConstraintError(f"routing_mode must be one of {list(ROUTING_MODES)}")
                parse_constraints(profile.get("routing_constraints"), catalog)
                parse_cost_band(profile.get("routing_cost_band"), catalog)
            except (CatalogError, ConstraintError) as exc:
                raise ValueError(f"routing profile '{name}' is invalid: {exc}") from exc

    # ------------------------------------------------------------------ request shape
    def prepare(self, body: Any) -> Prepared:
        """Validate a Model-Router-style body plus this router's optional extension fields.

        When `model` names a routing profile (Settings.profiles), the profile supplies any extension
        field the request leaves out; fields in the request win."""
        if not isinstance(body, dict):
            raise RoutingError(400, "invalid_request", "request body must be a JSON object")
        profile_name = body.get("model") if isinstance(body.get("model"), str) else None
        profile = self.settings.profiles.get(profile_name or "")
        if profile:
            body = {**profile, **body}
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
        pin = body.get("routing_pin", self.settings.pin_default)
        if not isinstance(pin, bool):
            raise RoutingError(400, "invalid_request", "routing_pin must be true or false")
        try:
            constraints = parse_constraints(body.get("routing_constraints"), catalog)
        except ConstraintError as exc:
            raise RoutingError(400, "invalid_routing_constraints", str(exc)) from exc
        try:
            cost_band = parse_cost_band(body.get("routing_cost_band"), catalog)
        except ConstraintError as exc:
            raise RoutingError(400, "invalid_routing_cost_band", str(exc)) from exc
        # `model` names the router deployment the client called; it is replaced per target.
        forwarded = {k: v for k, v in body.items() if k not in ROUTER_FIELDS and k != "model"}
        return Prepared(mode, catalog, constraints, forwarded, passthrough=not translation,
                        profile=profile_name if profile else None, pin=pin, cost_band=cost_band)

    def apply_policy(self, prepared: Prepared) -> Prepared:
        """Bound a prepared request by the caller's policy. Applied after the request's own fields."""
        policy = policy_for(self.policies, prepared.caller)
        if policy is None:
            return prepared
        if policy.catalogs and prepared.catalog.compatibility not in policy.catalogs:
            raise RoutingError(403, "policy_denied", f"this caller may use the {list(policy.catalogs)} catalog(s), "
                                                     f"not '{prepared.catalog.compatibility}'")
        if policy.modes and prepared.mode not in policy.modes:
            raise RoutingError(403, "policy_denied", f"this caller may use routing modes {list(policy.modes)}, "
                                                     f"not '{prepared.mode}'")
        return replace(prepared, policy=policy)

    # ------------------------------------------------------------------ stages 1 and 2
    async def route(self, prepared: Prepared, request_id: str) -> Routed:
        started = self._clock()
        catalog, mode, body = prepared.catalog, prepared.mode, prepared.body
        stage1 = select(catalog, mode, prepared.constraints, body, self.settings, passthrough=prepared.passthrough,
                        cost_band=prepared.cost_band, policy=prepared.policy)
        candidates = stage1.candidates
        event: dict[str, Any] = {
            "request_id": request_id,
            "routing_mode": mode,
            "compatibility": catalog.compatibility,
            "catalog_sha256": catalog.fingerprint,
            "routing_constraints": prepared.constraints.as_dict(),
            "cost_band": prepared.cost_band.as_dict() if prepared.cost_band else None,
            "policy": prepared.policy.as_dict() if prepared.policy else None,
            "claude_translation": not prepared.passthrough,
            "profile": prepared.profile,
            "caller": prepared.caller,
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
        pin_key, pinned, pin_log = await self._pin_lookup(prepared, stage1, candidates)
        if pin_log:
            event["pin"] = pin_log
        rendered = self.templates.render(
            candidates, mode=mode, constraints=prepared.constraints, cost_band=prepared.cost_band,
            requirements=stage1.requirements, body=body, chars_per_token=self.settings.chars_per_token,
            profile=prepared.profile, passthrough=prepared.passthrough, pin=pin_log, tokens=stage1.tokens)
        event["instructions"] = {"template_sha256": self.templates.fingerprint, "fragments": rendered.fragments,
                                 "dropped": rendered.dropped, "text": rendered.instructions}
        if pinned:
            names = [m.name for m in candidates]
            ranking = [n for n in pinned.ranking if n in names] + [n for n in names if n not in pinned.ranking]
            decision = Decision(names, ranking, {n: pinned.probabilities.get(n, 0.0) for n in ranking},
                                pinned.model, None, 0.0, skipped=True)
            order, low_confidence = ranking, False
        else:
            try:
                decision = await self.decision.decide(
                    build_state(body, self.settings.state_max_chars),
                    rendered.instructions,
                    rendered.criteria,
                    candidates,
                )
            except DecisionError as exc:
                if not self.settings.decision_fallback:
                    event.update(outcome="decision_failed", error_code=exc.code,
                                 total_latency_ms=round((self._clock() - started) * 1000, 3))
                    self.telemetry.record(event, body)
                    status = 504 if exc.code == "timeout" else 503 if exc.status_code == 429 else 502
                    raise RoutingError(status, "decision_unavailable", f"Decision-1 failed: {exc}") from exc
                # Degraded: stage 1's candidates in price order, cheapest first, without Decision-1's ranking.
                names = [m.name for m in candidates]
                decision = Decision(names, names, {n: 0.0 for n in names}, names[0], None, 0.0, skipped=True)
                event["degraded"] = {"reason": exc.code, "message": str(exc)}
                order, low_confidence = names, False
            else:
                threshold = 0.0 if decision.skipped else self.settings.threshold_for(mode)
                order, low_confidence = execution_order(decision.ranking, decision.probabilities, threshold, mode,
                                                        {m.name: m.tier for m in candidates})
        event["decision"] = {
            "pinned": pinned is not None,
            "deployment": self.decision.deployment,
            "ranking": decision.ranking,
            "execution_order": order,
            "top_probability": decision.probabilities[decision.ranking[0]],
            "low_confidence": low_confidence,
            "low_confidence_threshold": self.settings.threshold_for(mode),
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
                      prepared.passthrough, pin_key, rendered.instructions, rendered.criteria)

    async def _pin_lookup(self, prepared: Prepared, stage1, candidates: list[ModelEntry]):
        """(key, pin to follow or None, log entry or None) for this request."""
        if not prepared.pin:
            return None, None, None
        caller = prepared.caller or {}
        key = conversation_key(prepared.body, session_id=prepared.session_id,
                               scope=(caller.get("method"), caller.get("identity"),
                                      prepared.catalog.compatibility, prepared.mode, prepared.passthrough,
                                      prepared.cost_band))
        if key is None:
            return None, None, {"status": "no_key"}
        log: dict[str, Any] = {"key": key[:16], "source": "header" if prepared.session_id else "messages"}
        pin = await self.pins.get(key)
        if pin is None:
            return key, None, {**log, "status": "new"}
        if pin.model in {m.name for m in candidates}:
            return key, pin, {**log, "status": "followed", "model": pin.model}
        reason = next((r["reason"] for s in stage1.steps for r in s["removed"] if r["model"] == pin.model),
                      "not a candidate")
        return key, None, {**log, "status": "rerouted", "previous_model": pin.model, "reason": reason}

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
            send_body, attempt, dropped_any = body, 0, False
            if stream and not routed.passthrough and entry.api in USAGE_STREAM_APIS:
                # Ask for the final usage chunk so streamed requests are logged with usage and cost;
                # UsageTap removes it again if the caller did not ask for it.
                send_body = {**body, "stream_options": {**(body.get("stream_options") or {}), "include_usage": True}}
            while attempt < settings.attempts_per_model:
                attempt += 1
                remaining = deadline - self._clock()
                if remaining <= 0:
                    raise RoutingError(504, "deadline_exceeded", "no model answered within the time budget", attempts)
                t0 = self._clock()
                record: dict[str, Any] = {"model": name, "attempt": attempt, "status": None}
                try:
                    response = await self.chat.send(entry, send_body, passthrough=routed.passthrough,
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
                    if response.status_code in (401, 403):
                        # The router's own credentials were refused (for example a missing role on one
                        # deployment). The caller's credentials are not involved, so passing this status
                        # through would mislead them: try the next model, and end in 502 if none answers.
                        record["outcome"] = "upstream_auth_error"
                        attempts.append(record)
                        await response.aclose()
                        break
                    if not _retryable(response.status_code):
                        content = await response.aread()
                        await response.aclose()
                        rejected = [] if routed.passthrough else _rejected_params(content, send_body)
                        if response.status_code == 400 and rejected and not dropped_any:
                            # Optional sampling/reasoning parameters are accepted from every caller; a model
                            # that refuses one gets the request again without it, as if it had ignored it.
                            record.update(outcome="parameter_rejected", dropped=rejected)
                            attempts.append(record)
                            send_body = {k: v for k, v in send_body.items() if k not in rejected}
                            attempt -= 1  # dropping a parameter does not use up one of the model's attempts
                            dropped_any = True
                            continue
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
        if routed.pin_key and outcome == "success" and served:
            self.pins.set_nowait(routed.pin_key, served.name, routed.order, routed.decision.probabilities)
            routed.event.setdefault("pin", {})["pinned_to"] = served.name
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

        tap = UsageTap(keep_usage_chunk=bool((body.get("stream_options") or {}).get("include_usage")))

        async def chunks() -> AsyncIterator[bytes]:
            outcome = "success"
            try:
                async for chunk in response.aiter_raw():
                    out = tap.feed(chunk)
                    if out:
                        yield out
                rest = tap.finish()
                if rest:
                    yield rest
            except BaseException:  # includes the client disconnecting (GeneratorExit / cancellation)
                outcome = "stream_interrupted"
                raise
            finally:
                await response.aclose()
                self._finish(routed, body, served, attempts, outcome,
                             {"usage": tap.usage} if tap.usage else None)

        return StreamingCompletion(routed, served, response.status_code,
                                   response.headers.get("content-type", "text/event-stream"), attempts, chunks())

    async def close(self) -> None:
        await self.pins.close()
        await self.decision.close()
        await self.chat.close()
        await self.decision.auth.close()
        if self.chat.auth is not self.decision.auth:
            await self.chat.auth.close()
        self.telemetry.close()
