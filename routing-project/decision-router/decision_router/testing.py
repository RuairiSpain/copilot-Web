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
    def __init__(self, pool: list[str], router_deployments: dict[str, list[str]] | None = None,
                 position_bias: float = 0.0):
        self.pool = pool
        # > 0 makes the fake Decision-1 favour whichever option is listed first, to exercise the option-order test.
        self.position_bias = position_bias
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
        if request.url.path.endswith("/anthropic/v1/messages"):
            return self._messages(request, body)
        return httpx.Response(404, json={"error": {"code": "not_found"}})

    def _decision(self, body: dict[str, Any]) -> httpx.Response:
        if self.decision_responses:
            queued = self.decision_responses.popleft()
            if callable(queued):  # built from the request, e.g. to rank whatever options were offered
                queued = queued(body)
            return queued if isinstance(queued, httpx.Response) else httpx.Response(200, json=queued)
        question = body["questions"]["route"]
        options = list(question["criteria"])
        raw = {o: _score(body["state"], o) ** 3 for o in options}
        if self.position_bias:
            raw[options[0]] *= 1 + self.position_bias
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

    def _messages(self, request: httpx.Request, body: dict[str, Any]) -> httpx.Response:
        """Anthropic Messages API, as Claude deployments on Foundry expose it."""
        deployment = body.get("model", "")
        queue = self.failures.get(deployment)
        if queue:
            status = queue.popleft()
            if status == 0:
                raise httpx.ConnectError("simulated network failure", request=request)
            return httpx.Response(status, json={"type": "error", "error": {"type": "overloaded_error" if status >= 500
                                                                         else "invalid_request_error",
                                                                         "message": "simulated"}})
        tools = body.get("tools") or []
        if tools:
            content = [{"type": "tool_use", "id": "toolu_fake", "name": tools[0]["name"], "input": {"q": "x"}}]
            stop = "tool_use"
        else:
            content = [{"type": "text", "text": f"answer from {deployment}"}]
            stop = "end_turn"
        if body.get("stream"):
            events = [
                ("message_start", {"type": "message_start", "message": {"id": "msg_fake", "model": deployment,
                                                                        "usage": {"input_tokens": 12}}}),
            ]
            for index, block in enumerate(content):
                if block["type"] == "text":
                    events += [("content_block_start", {"type": "content_block_start", "index": index,
                                                        "content_block": {"type": "text", "text": ""}}),
                               ("content_block_delta", {"type": "content_block_delta", "index": index,
                                                        "delta": {"type": "text_delta", "text": block["text"]}})]
                else:
                    events += [("content_block_start", {"type": "content_block_start", "index": index,
                                                        "content_block": {"type": "tool_use", "id": block["id"],
                                                                          "name": block["name"], "input": {}}}),
                               ("content_block_delta", {"type": "content_block_delta", "index": index,
                                                        "delta": {"type": "input_json_delta",
                                                                  "partial_json": json.dumps(block["input"])}})]
                events.append(("content_block_stop", {"type": "content_block_stop", "index": index}))
            events += [("message_delta", {"type": "message_delta", "delta": {"stop_reason": stop},
                                          "usage": {"output_tokens": 5}}),
                       ("message_stop", {"type": "message_stop"})]
            payload = "".join(f"event: {name}\ndata: {json.dumps(data)}\n\n" for name, data in events)
            return httpx.Response(200, stream=_Chunks(payload.encode()), headers={"content-type": "text/event-stream"})
        return httpx.Response(200, json={"id": "msg_fake", "type": "message", "role": "assistant", "model": deployment,
                                         "content": content, "stop_reason": stop,
                                         "usage": {"input_tokens": 12, "output_tokens": 5}})


def ranked(order: list[str], top: float = 0.6):
    """A queued Decision-1 answer that ranks `order` first (in that order) among whatever options are offered."""
    def build(body: dict[str, Any]) -> dict[str, Any]:
        options = list(body["questions"]["route"]["criteria"])
        listed = [o for o in order if o in options]
        rest = [o for o in options if o not in listed]
        probabilities: dict[str, float] = {}
        remaining = 1.0
        share = top
        for name in listed:
            probabilities[name] = share
            remaining -= share
            share = remaining / 2
        for name in rest:
            probabilities[name] = remaining / max(1, len(rest)) / 4
        total = sum(probabilities.values())
        probabilities = {k: v / total for k, v in probabilities.items()}
        return {"answers": {"route": {"type": "choice", "choice": listed[0] if listed else options[0],
                                      "probabilities": probabilities, "confidence": top}}}
    return build
