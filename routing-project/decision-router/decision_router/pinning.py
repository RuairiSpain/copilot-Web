"""Conversation pinning: route a conversation's first turn, then keep it on the model that answered.

An agent loop sends one request per step, each carrying the whole history. Routing every step
independently can move a task between models mid-way: the next model loses the previous one's
reasoning, writes in a different style, and gets no prompt-cache hits. With pinning on
(`routing_pin: true`, a profile, or ROUTER_PIN_DEFAULT=true):

1. The router derives a key for the conversation (below).
2. No pin yet: the request is routed as usual. When a model answers, the conversation is pinned
   to it, with the rest of that turn's ranking kept for fallback.
3. Pinned: Decision-1 is not called. The pinned model is called first; if it fails, the other
   models from the pinned ranking that still pass stage 1 are tried in order, and whichever
   answers becomes the new pin.
4. Stage 1 still runs on every turn. If the pinned model no longer passes (the request now has
   tools or images it can't take, or the conversation outgrew its context window), the turn is
   routed afresh and the conversation is re-pinned.

The conversation key is, in order of preference:
- the `x-router-session-id` header, when the caller can send one;
- otherwise a SHA-256 of the first system/developer message, the first user message and the
  `user` field. Every turn of one conversation repeats those, so they identify it. Two
  conversations that open with exactly the same instructions and first message share a pin;
  the effect is only that both use the same model.
Either way the key also includes the caller's identity, the catalog and the routing mode, so
callers, profiles and modes never share pins.

Pins expire ROUTER_PIN_TTL_SECONDS after last use. Where they are kept:
- by default, in this process's memory (at most ROUTER_PIN_MAX_ENTRIES, least recently used
  dropped first). With several replicas or worker processes a conversation keeps its pin only
  while its requests reach the same process;
- with ROUTER_PIN_STORE_URL=redis://... or rediss://... (Azure Cache for Redis), in Redis, shared
  by every replica. Reads are awaited; writes are sent in the background. A Redis error never
  fails a request: a failed read counts as "no pin", so the turn is routed afresh.
"""
from __future__ import annotations

import asyncio
import hashlib
import json
import logging
import time
from collections import OrderedDict
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any

SESSION_HEADER = "x-router-session-id"
log = logging.getLogger("decision_router.pinning")


def _first(messages: list[Any], roles: tuple[str, ...]) -> Any:
    for message in messages:
        if isinstance(message, dict) and message.get("role") in roles:
            return message.get("content")
    return None


def conversation_key(body: dict[str, Any], *, session_id: str | None, scope: tuple[Any, ...]) -> str | None:
    """A stable key for the conversation `body` belongs to, or None if it can't be identified."""
    if session_id:
        basis: Any = {"session": session_id[:256]}
    else:
        messages = body.get("messages") or []
        first_user = _first(messages, ("user",))
        if first_user is None:
            return None
        system = body.get("system") if "system" in body else _first(messages, ("system", "developer"))
        basis = {"system": system, "user": first_user, "end_user": body.get("user")}
    raw = json.dumps({"scope": list(scope), "basis": basis}, sort_keys=True, ensure_ascii=False, default=str)
    return hashlib.sha256(raw.encode()).hexdigest()


@dataclass
class Pin:
    model: str
    ranking: list[str]  # the order models were tried in on the turn that set the pin
    probabilities: dict[str, float]
    expires: float


def _ranking(model: str, ranking: list[str]) -> list[str]:
    return [model, *(n for n in ranking if n != model)]


class PinStore:
    """In-process pins."""

    def __init__(self, ttl_seconds: float = 3600, max_entries: int = 10_000,
                 clock: Callable[[], float] = time.monotonic):
        self.ttl = ttl_seconds
        self.max_entries = max_entries
        self._clock = clock
        self._pins: OrderedDict[str, Pin] = OrderedDict()

    async def get(self, key: str) -> Pin | None:
        return self.get_now(key)

    def set_nowait(self, key: str, model: str, ranking: list[str], probabilities: dict[str, float]) -> None:
        self.set(key, model, ranking, probabilities)

    async def close(self) -> None:
        pass

    def get_now(self, key: str) -> Pin | None:
        pin = self._pins.get(key)
        if pin is None:
            return None
        now = self._clock()
        if pin.expires <= now:
            del self._pins[key]
            return None
        pin.expires = now + self.ttl  # sliding expiry: an active conversation keeps its pin
        self._pins.move_to_end(key)
        return pin

    def set(self, key: str, model: str, ranking: list[str], probabilities: dict[str, float]) -> None:
        self._pins[key] = Pin(model, _ranking(model, ranking), dict(probabilities), self._clock() + self.ttl)
        self._pins.move_to_end(key)
        while len(self._pins) > self.max_entries:
            self._pins.popitem(last=False)  # least recently used

    def __len__(self) -> int:
        return len(self._pins)


class RedisPinStore:
    """Pins shared by every replica, in Redis (key prefix `decision-router:pin:`, sliding TTL)."""

    PREFIX = "decision-router:pin:"

    def __init__(self, url: str, ttl_seconds: float = 3600, client: Any = None):
        if client is None:
            import redis.asyncio as redis  # only needed when ROUTER_PIN_STORE_URL is set

            client = redis.from_url(url, socket_timeout=1.0, socket_connect_timeout=1.0)
        self.redis = client
        self.ttl = max(1, int(ttl_seconds))
        self._tasks: set[asyncio.Task] = set()

    async def get(self, key: str) -> Pin | None:
        try:
            raw = await self.redis.get(self.PREFIX + key)
            if raw is None:
                return None
            await self.redis.expire(self.PREFIX + key, self.ttl)  # sliding expiry
            data = json.loads(raw)
            return Pin(data["model"], list(data["ranking"]), dict(data["probabilities"]), 0.0)
        except Exception as exc:  # noqa: BLE001 - a store failure must not fail the request
            log.warning("pin store read failed: %s", exc)
            return None

    async def set(self, key: str, model: str, ranking: list[str], probabilities: dict[str, float]) -> None:
        value = json.dumps({"model": model, "ranking": _ranking(model, ranking), "probabilities": probabilities})
        try:
            await self.redis.set(self.PREFIX + key, value, ex=self.ttl)
        except Exception as exc:  # noqa: BLE001
            log.warning("pin store write failed: %s", exc)

    def set_nowait(self, key: str, model: str, ranking: list[str], probabilities: dict[str, float]) -> None:
        task = asyncio.get_running_loop().create_task(self.set(key, model, ranking, probabilities))
        self._tasks.add(task)
        task.add_done_callback(self._tasks.discard)

    async def ping(self) -> bool:
        try:
            return bool(await self.redis.ping())
        except Exception:  # noqa: BLE001
            return False

    async def close(self) -> None:
        if self._tasks:
            await asyncio.gather(*self._tasks, return_exceptions=True)
        await self.redis.aclose()


def build_pin_store(url: str | None, ttl_seconds: float, max_entries: int,
                    clock: Callable[[], float] = time.monotonic) -> PinStore | RedisPinStore:
    if url:
        return RedisPinStore(url, ttl_seconds)
    return PinStore(ttl_seconds, max_entries, clock=clock)
