"""An in-memory stand-in for Foundry: Decision-1, model deployments and Model Router deployments.

Used by the tests and by `compare_with_model_router.py --dry-run`, so the whole flow can be
exercised without credentials. Its answers are deterministic but carry no meaning.
"""
from __future__ import annotations

import hashlib
import json
from collections import defaultdict, deque
from typing import Any

import httpx


def _score(*parts: str) -> float:
    digest = hashlib.sha256("|".join(parts).encode("utf-8")).digest()
    return (int.from_bytes(digest[:8], "big") + 1) / 2**64


class _Chunks(httpx.AsyncByteStream):
    """An unread body, like a real network stream (httpx reads plain `content=` eagerly)."""

    def __init__(self, data: bytes):
        self.data = data

    async def __aiter__(self):
        for i in range(0, len(self.data), 16):
            yield self.data[i:i + 16]


class FakeFoundry:
    def __init__(self, pool: list[str], router_deployments: dict[str, list[str]] | None = None):
        self.pool = pool
        # Model Router deployment name -> models it may route to.
        self.router_deployments = router_deployments or {}
        self.failures: dict[str, deque] = defaultdict(deque)
        self.decision_responses: deque = deque()
        self.requests: list[dict[str, Any]] = []

    def fail(self, deployment: str, *statuses: int) -> None:
        """Queue HTTP statuses for a deployment's next calls (0 means a network error)."""
        self.failures[deployment].extend(statuses)

    def transport(self) -> httpx.MockTransport:
        return httpx.MockTransport(self.handle)

    def handle(self, request: httpx.Request) -> httpx.Response:
        body = json.loads(request.content or b"{}")
        self.requests.append({"path": request.url.path, "headers": dict(request.headers), "body": body})
        if request.url.path.endswith("/systemone"):
            return self._decision(body)
        if request.url.path.endswith("/chat/completions"):
            return self._chat(request, body)
        return httpx.Response(404, json={"error": {"code": "not_found"}})

    def _decision(self, body: dict[str, Any]) -> httpx.Response:
        if self.decision_responses:
            queued = self.decision_responses.popleft()
            return queued if isinstance(queued, httpx.Response) else httpx.Response(200, json=queued)
        question = body["questions"]["route"]
        options = list(question["criteria"])
        raw = {o: _score(body["state"], o) ** 3 for o in options}
        total = sum(raw.values())
        probabilities = {o: v / total for o, v in raw.items()}
        choice = max(probabilities, key=probabilities.get)
        tokens = (len(body["state"]) + len(json.dumps(question))) // 4
        return httpx.Response(200, json={
            "model": "microsoft-decision-1",
            "usage": {"prompt_tokens": tokens, "total_tokens": tokens},
            "answers": {"route": {"type": "choice", "choice": choice, "probabilities": probabilities,
                                  "confidence": probabilities[choice]}},
        })

    def _chat(self, request: httpx.Request, body: dict[str, Any]) -> httpx.Response:
        deployment = body.get("model", "")
        queue = self.failures.get(deployment)
        if queue:
            status = queue.popleft()
            if status == 0:
                raise httpx.ConnectError("simulated network failure", request=request)
            return httpx.Response(status, json={"error": {"code": str(status), "message": "simulated"}})
        served = deployment
        if deployment in self.router_deployments:
            choices = self.router_deployments[deployment]
            text = json.dumps(body.get("messages"), sort_keys=True)
            served = max(choices, key=lambda m: _score(text, m)) + "-2026-01-01"
        prompt_tokens = sum(len(str(m.get("content", "")).split()) for m in body.get("messages", [])) + 8
        if body.get("stream"):
            events = [
                {"id": "fake", "object": "chat.completion.chunk", "model": served,
                 "choices": [{"index": 0, "delta": {"content": f"answer from {served}"}}]},
            ]
            payload = "".join(f"data: {json.dumps(e)}\n\n" for e in events) + "data: [DONE]\n\n"
            return httpx.Response(200, stream=_Chunks(payload.encode()), headers={"content-type": "text/event-stream"})
        return httpx.Response(200, json={
            "id": "chatcmpl-fake",
            "object": "chat.completion",
            "model": served,
            "choices": [{"index": 0, "finish_reason": "stop",
                         "message": {"role": "assistant", "content": f"answer from {served}"}}],
            "usage": {"prompt_tokens": prompt_tokens, "completion_tokens": 5, "total_tokens": prompt_tokens + 5},
        })
