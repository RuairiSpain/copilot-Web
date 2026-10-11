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

Pins live in this process's memory (ROUTER_PIN_TTL_SECONDS after last use, at most
ROUTER_PIN_MAX_ENTRIES). With several replicas, a conversation keeps its pin only while its
requests reach the same replica; use session affinity on the load balancer, or accept that a
replica switch re-routes once and pins again.
"""
from __future__ import annotations

import hashlib
import json
import time
from collections import OrderedDict
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any

SESSION_HEADER = "x-router-session-id"


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


class PinStore:
    def __init__(self, ttl_seconds: float = 3600, max_entries: int = 10_000,
                 clock: Callable[[], float] = time.monotonic):
        self.ttl = ttl_seconds
        self.max_entries = max_entries
        self._clock = clock
        self._pins: OrderedDict[str, Pin] = OrderedDict()

    def get(self, key: str) -> Pin | None:
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
        self._pins[key] = Pin(model, [model, *(n for n in ranking if n != model)], dict(probabilities),
                              self._clock() + self.ttl)
        self._pins.move_to_end(key)
        while len(self._pins) > self.max_entries:
            self._pins.popitem(last=False)  # least recently used

    def __len__(self) -> int:
        return len(self._pins)
