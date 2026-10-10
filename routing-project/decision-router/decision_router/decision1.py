"""Stage 2: ask Microsoft-Decision-1 to choose among the stage-1 candidates.

Request and response follow the Foundry SystemOne route:

    POST https://<resource>.services.ai.azure.com/providers/microsoft/v1/systemone
    {"model": "<deployment>", "state": "<text>",
     "questions": {"route": {"type": "choice", "instructions": "...",
                             "criteria": {"<model>": "<description>", ...}}}}

    {"answers": {"route": {"type": "choice", "choice": "<model>",
                           "probabilities": {"<model>": 0.7, ...}, "confidence": 0.9}}}
"""
from __future__ import annotations

import math
import time
from dataclasses import dataclass, field
from typing import Any

import httpx

from .auth import FoundryAuth
from .catalog import ModelEntry

QUESTION_NAME = "route"
MAX_CHOICE_OPTIONS = 255
PROBABILITY_TOLERANCE = 1e-4


class DecisionError(RuntimeError):
    def __init__(self, code: str, message: str, status_code: int | None = None):
        super().__init__(message)
        self.code = code
        self.status_code = status_code


@dataclass
class Decision:
    candidates: list[str]
    ranking: list[str]
    probabilities: dict[str, float]
    choice: str
    confidence: float | None
    latency_ms: float
    skipped: bool = False
    request: dict[str, Any] = field(default_factory=dict)


def _text(content: Any) -> str:
    if content is None:
        return ""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts = []
        for part in content:
            if isinstance(part, dict):
                kind = part.get("type")
                if kind == "text":
                    parts.append(str(part.get("text", "")))
                elif kind:
                    parts.append(f"[{kind} attachment]")
            elif isinstance(part, str):
                parts.append(part)
        return "\n".join(p for p in parts if p)
    return str(content)


def _render_message(message: dict[str, Any]) -> str:
    role = str(message.get("role", "user"))
    body = _text(message.get("content"))
    calls = message.get("tool_calls") or []
    if calls:
        names = [str((c.get("function") or {}).get("name", "?")) for c in calls if isinstance(c, dict)]
        body = (body + "\n" if body else "") + f"[calls tools: {', '.join(names)}]"
    return f"[{role}]\n{body}"


def _clip_middle(text: str, limit: int) -> str:
    if len(text) <= limit:
        return text
    marker = f"\n... [{len(text) - limit} characters omitted] ...\n"
    keep = max(0, limit - len(marker))
    head = keep * 3 // 5
    return text[:head] + marker + text[len(text) - (keep - head):]


def build_state(body: dict[str, Any], max_chars: int) -> str:
    """Render a chat-completions body as the text Decision-1 judges.

    When the conversation is too long, the system prompt keeps up to a fifth of the
    budget and the rest goes to the most recent messages.
    """
    messages = [m for m in body.get("messages", []) if isinstance(m, dict)]
    facts = [f"{len(messages)} message(s)"]
    tools = [t.get("function", {}).get("name") for t in body.get("tools") or [] if isinstance(t, dict)]
    if tools:
        facts.append("tools offered: " + ", ".join(str(t) for t in tools if t))
    fmt = body.get("response_format")
    if isinstance(fmt, dict) and fmt.get("type"):
        facts.append(f"response format: {fmt['type']}")
    limit = body.get("max_completion_tokens") or body.get("max_tokens")
    if limit:
        facts.append(f"output limit: {limit} tokens")
    header = "Request: " + "; ".join(facts) + "\nConversation, oldest first:\n\n"

    blocks = [_render_message(m) for m in messages]
    budget = max_chars - len(header)
    if sum(len(b) + 2 for b in blocks) <= budget:
        return header + "\n\n".join(blocks)

    head: list[str] = []
    if messages and messages[0].get("role") in {"system", "developer"}:
        head.append(_clip_middle(blocks[0], budget // 5))
        blocks = blocks[1:]
    remaining = budget - sum(len(b) + 2 for b in head)
    tail: list[str] = []
    for block in reversed(blocks):
        if len(block) + 2 <= remaining:
            tail.insert(0, block)
            remaining -= len(block) + 2
        elif not tail:
            tail.insert(0, _clip_middle(block, max(200, remaining - 2)))
            remaining = 0
        else:
            break
    omitted = len(blocks) - len(tail)
    note = [f"[{omitted} earlier message(s) omitted]"] if omitted else []
    return header + "\n\n".join(head + note + tail)


def build_request(deployment: str, state: str, instructions: str, criteria: dict[str, str]) -> dict[str, Any]:
    return {
        "model": deployment,
        "state": state,
        "questions": {
            QUESTION_NAME: {"type": "choice", "instructions": instructions, "criteria": criteria},
        },
    }


def parse_answer(body: Any, options: list[str]) -> tuple[str, dict[str, float], float | None]:
    if not isinstance(body, dict) or not isinstance(body.get("answers"), dict):
        raise DecisionError("invalid_response", "Decision-1 response has no 'answers' object")
    answer = body["answers"].get(QUESTION_NAME)
    if not isinstance(answer, dict):
        raise DecisionError("invalid_response", f"Decision-1 response has no '{QUESTION_NAME}' answer")
    if answer.get("type") == "refusal":
        raise DecisionError("refusal", "Decision-1 refused to answer")
    if answer.get("type") != "choice":
        raise DecisionError("invalid_response", f"expected a choice answer, got {answer.get('type')!r}")
    raw = answer.get("probabilities")
    if not isinstance(raw, dict) or set(raw) != set(options):
        raise DecisionError("invalid_response", "probabilities must cover exactly the offered models")
    probabilities: dict[str, float] = {}
    for key, value in raw.items():
        if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or not 0 <= value <= 1:
            raise DecisionError("invalid_response", f"invalid probability for {key!r}")
        probabilities[key] = float(value)
    if abs(sum(probabilities.values()) - 1.0) > PROBABILITY_TOLERANCE:
        raise DecisionError("invalid_response", "probabilities do not sum to 1")
    choice = answer.get("choice")
    if choice not in probabilities:
        raise DecisionError("invalid_response", "choice is not one of the offered models")
    confidence = answer.get("confidence")
    if confidence is not None and (not isinstance(confidence, (int, float)) or not 0 <= confidence <= 1):
        raise DecisionError("invalid_response", "invalid confidence")
    return choice, probabilities, None if confidence is None else float(confidence)


def rank(probabilities: dict[str, float], candidates: list[ModelEntry]) -> list[str]:
    """Highest probability first; ties go to the cheaper model."""
    tier = {m.name: m.tier for m in candidates}
    return sorted(probabilities, key=lambda name: (-probabilities[name], tier[name]))


class Decision1Client:
    def __init__(self, url: str, deployment: str, auth: FoundryAuth, *, timeout_seconds: float = 10.0,
                 http: httpx.AsyncClient | None = None):
        self.url = url
        self.deployment = deployment
        self.auth = auth
        self.timeout_seconds = timeout_seconds
        self._http = http or httpx.AsyncClient()
        self._owns_http = http is None

    async def decide(self, state: str, instructions: str, criteria: dict[str, str],
                     candidates: list[ModelEntry]) -> Decision:
        names = [m.name for m in candidates]
        started = time.perf_counter()
        if len(names) == 1:
            # The choice primitive needs at least two options; stage 1 already decided.
            return Decision(names, names, {names[0]: 1.0}, names[0], 1.0, 0.0, skipped=True)
        if len(names) > MAX_CHOICE_OPTIONS:
            raise DecisionError("too_many_options", f"Decision-1 accepts at most {MAX_CHOICE_OPTIONS} options")
        request = build_request(self.deployment, state, instructions, criteria)
        try:
            response = await self._http.post(
                self.url, json=request, headers=await self.auth.headers(), timeout=self.timeout_seconds,
            )
        except httpx.TimeoutException as exc:
            raise DecisionError("timeout", "Decision-1 timed out") from exc
        except httpx.HTTPError as exc:
            raise DecisionError("unreachable", f"Decision-1 request failed: {type(exc).__name__}") from exc
        if response.status_code >= 400:
            raise DecisionError("http_error", f"Decision-1 returned HTTP {response.status_code}",
                                status_code=response.status_code)
        try:
            body = response.json()
        except ValueError as exc:
            raise DecisionError("invalid_response", "Decision-1 response is not JSON") from exc
        choice, probabilities, confidence = parse_answer(body, names)
        ranking = rank(probabilities, candidates)
        return Decision(names, ranking, probabilities, choice, confidence,
                        round((time.perf_counter() - started) * 1000, 3), request=request)

    async def close(self) -> None:
        if self._owns_http:
            await self._http.aclose()
