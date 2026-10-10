"""Translate between OpenAI Chat Completions and the Anthropic Messages API used by Claude on Foundry.

Clients send this router the same chat-completions body they would send Model Router, and get a
chat-completions response back, whichever model answers. Claude deployments on Foundry accept
only the Messages API (POST https://<resource>.services.ai.azure.com/anthropic/v1/messages), so
requests to Claude are converted here and the answer converted back.

Translated: system/developer messages, text and image content, assistant tool calls, tool results,
tools, tool_choice, parallel_tool_calls, max_tokens/max_completion_tokens, temperature, top_p,
stop, stream (with stream_options.include_usage), and user (as metadata.user_id).
Not translated: response_format, n>1, logprobs, seed, presence/frequency penalties. Stage 1 keeps
requests that need structured output away from Claude; the other fields are dropped.
"""
from __future__ import annotations

import json
import time
import uuid
from collections.abc import AsyncIterator
from typing import Any

import httpx

FINISH_REASONS = {"end_turn": "stop", "stop_sequence": "stop", "max_tokens": "length", "tool_use": "tool_calls",
                  "refusal": "content_filter", "pause_turn": "stop"}


def _image_block(url: str) -> dict[str, Any]:
    if url.startswith("data:"):
        header, _, data = url.partition(",")
        media_type = header[5:].split(";")[0] or "image/png"
        return {"type": "image", "source": {"type": "base64", "media_type": media_type, "data": data}}
    return {"type": "image", "source": {"type": "url", "url": url}}


def _content_blocks(content: Any) -> list[dict[str, Any]]:
    if content is None:
        return []
    if isinstance(content, str):
        return [{"type": "text", "text": content}] if content else []
    blocks = []
    for part in content:
        if isinstance(part, str):
            blocks.append({"type": "text", "text": part})
        elif part.get("type") == "text":
            blocks.append({"type": "text", "text": part.get("text", "")})
        elif part.get("type") == "image_url":
            image = part.get("image_url") or {}
            blocks.append(_image_block(image.get("url", "") if isinstance(image, dict) else str(image)))
    return blocks


def _text(content: Any) -> str:
    return "\n".join(b["text"] for b in _content_blocks(content) if b["type"] == "text")


def to_messages_request(body: dict[str, Any], deployment: str, default_max_tokens: int) -> dict[str, Any]:
    system_parts: list[str] = []
    messages: list[dict[str, Any]] = []

    def append(role: str, blocks: list[dict[str, Any]]) -> None:
        if not blocks:
            return
        if messages and messages[-1]["role"] == role:  # Messages API requires alternating roles
            messages[-1]["content"].extend(blocks)
        else:
            messages.append({"role": role, "content": blocks})

    for message in body.get("messages", []):
        role = message.get("role")
        if role in ("system", "developer"):
            system_parts.append(_text(message.get("content")))
        elif role == "user":
            append("user", _content_blocks(message.get("content")))
        elif role == "assistant":
            blocks = _content_blocks(message.get("content"))
            for call in message.get("tool_calls") or []:
                function = call.get("function") or {}
                try:
                    arguments = json.loads(function.get("arguments") or "{}")
                except json.JSONDecodeError:
                    arguments = {"_raw": function.get("arguments")}
                blocks.append({"type": "tool_use", "id": call.get("id") or f"toolu_{uuid.uuid4().hex[:24]}",
                               "name": function.get("name", ""), "input": arguments})
            append("assistant", blocks)
        elif role == "tool":
            append("user", [{"type": "tool_result", "tool_use_id": message.get("tool_call_id", ""),
                             "content": _text(message.get("content"))}])

    request: dict[str, Any] = {
        "model": deployment,
        "messages": messages,
        "max_tokens": body.get("max_completion_tokens") or body.get("max_tokens") or default_max_tokens,
    }
    if system_parts:
        request["system"] = "\n\n".join(p for p in system_parts if p)
    for key in ("temperature", "top_p"):
        if body.get(key) is not None:
            request[key] = body[key]
    stop = body.get("stop")
    if stop:
        request["stop_sequences"] = [stop] if isinstance(stop, str) else list(stop)
    if body.get("stream"):
        request["stream"] = True
    if body.get("user"):
        request["metadata"] = {"user_id": str(body["user"])}

    tools = [t.get("function") or {} for t in body.get("tools") or [] if t.get("type", "function") == "function"]
    if tools:
        request["tools"] = [{"name": f.get("name", ""), "description": f.get("description", ""),
                             "input_schema": f.get("parameters") or {"type": "object", "properties": {}}}
                            for f in tools]
        choice = body.get("tool_choice")
        if choice == "required":
            tool_choice: dict[str, Any] = {"type": "any"}
        elif choice == "none":
            tool_choice = {"type": "none"}
        elif isinstance(choice, dict) and choice.get("type") == "function":
            tool_choice = {"type": "tool", "name": (choice.get("function") or {}).get("name", "")}
        else:
            tool_choice = {"type": "auto"}
        if body.get("parallel_tool_calls") is False and tool_choice["type"] != "none":
            tool_choice["disable_parallel_tool_use"] = True
        request["tool_choice"] = tool_choice
    return request


def chat_usage(usage: dict[str, Any] | None) -> dict[str, Any]:
    usage = usage or {}
    cached = int(usage.get("cache_read_input_tokens") or 0)
    prompt = int(usage.get("input_tokens") or 0) + cached + int(usage.get("cache_creation_input_tokens") or 0)
    completion = int(usage.get("output_tokens") or 0)
    return {"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": prompt + completion,
            "prompt_tokens_details": {"cached_tokens": cached}}


def from_messages_response(message: dict[str, Any]) -> dict[str, Any]:
    text = [b.get("text", "") for b in message.get("content", []) if b.get("type") == "text"]
    calls = [{"id": b.get("id"), "type": "function",
              "function": {"name": b.get("name"), "arguments": json.dumps(b.get("input") or {})}}
             for b in message.get("content", []) if b.get("type") == "tool_use"]
    reply: dict[str, Any] = {"role": "assistant", "content": "".join(text) if text else None}
    if calls:
        reply["tool_calls"] = calls
    return {
        "id": message.get("id") or f"chatcmpl-{uuid.uuid4().hex}",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": message.get("model"),
        "choices": [{"index": 0, "message": reply,
                     "finish_reason": FINISH_REASONS.get(message.get("stop_reason") or "", "stop")}],
        "usage": chat_usage(message.get("usage")),
    }


def error_body(content: bytes, status: int) -> bytes:
    """Anthropic error JSON -> OpenAI error JSON, so clients see one error shape."""
    try:
        error = json.loads(content).get("error") or {}
    except (ValueError, AttributeError):
        error = {}
    return json.dumps({"error": {"message": error.get("message") or f"Claude returned HTTP {status}",
                                 "type": error.get("type") or "upstream_error", "code": str(status)}}).encode()


class ChatCompletionStream(httpx.AsyncByteStream):
    """Re-emits an Anthropic Messages event stream as chat.completion.chunk server-sent events."""

    def __init__(self, upstream: httpx.Response, include_usage: bool = False):
        self.upstream = upstream
        self.include_usage = include_usage

    async def __aiter__(self) -> AsyncIterator[bytes]:
        chunk_id, model, created = f"chatcmpl-{uuid.uuid4().hex}", None, int(time.time())
        usage: dict[str, Any] = {}
        tool_index: dict[int, int] = {}

        def chunk(delta: dict[str, Any], finish: str | None = None) -> bytes:
            payload = {"id": chunk_id, "object": "chat.completion.chunk", "created": created, "model": model,
                       "choices": [{"index": 0, "delta": delta, "finish_reason": finish}]}
            return f"data: {json.dumps(payload)}\n\n".encode()

        event = None
        async for line in self.upstream.aiter_lines():
            if line.startswith("event:"):
                event = line[6:].strip()
                continue
            if not line.startswith("data:"):
                continue
            data = json.loads(line[5:].strip() or "{}")
            kind = data.get("type", event)
            if kind == "message_start":
                message = data.get("message") or {}
                chunk_id, model = message.get("id") or chunk_id, message.get("model")
                usage.update(message.get("usage") or {})
                yield chunk({"role": "assistant", "content": ""})
            elif kind == "content_block_start":
                block = data.get("content_block") or {}
                if block.get("type") == "tool_use":
                    index = tool_index.setdefault(data.get("index", 0), len(tool_index))
                    yield chunk({"tool_calls": [{"index": index, "id": block.get("id"), "type": "function",
                                                 "function": {"name": block.get("name"), "arguments": ""}}]})
            elif kind == "content_block_delta":
                delta = data.get("delta") or {}
                if delta.get("type") == "text_delta":
                    yield chunk({"content": delta.get("text", "")})
                elif delta.get("type") == "input_json_delta":
                    index = tool_index.get(data.get("index", 0), 0)
                    yield chunk({"tool_calls": [{"index": index,
                                                 "function": {"arguments": delta.get("partial_json", "")}}]})
            elif kind == "message_delta":
                usage.update(data.get("usage") or {})
                stop = (data.get("delta") or {}).get("stop_reason")
                yield chunk({}, FINISH_REASONS.get(stop or "", "stop"))
            elif kind == "error":
                error = data.get("error") or {}
                yield f"data: {json.dumps({'error': {'message': error.get('message'), 'type': error.get('type')}})}\n\n".encode()
        if self.include_usage:
            payload = {"id": chunk_id, "object": "chat.completion.chunk", "created": created, "model": model,
                       "choices": [], "usage": chat_usage(usage)}
            yield f"data: {json.dumps(payload)}\n\n".encode()
        yield b"data: [DONE]\n\n"

    async def aclose(self) -> None:
        await self.upstream.aclose()
