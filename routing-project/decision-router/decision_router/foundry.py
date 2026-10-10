"""Calls the chosen Foundry deployment and always returns a chat-completions shaped response.

openai_chat and mai_chat models take the client's body as is (with `model` set to the deployment);
anthropic_messages models (Claude) go through the translation in anthropic.py.
"""
from __future__ import annotations

import json
from typing import Any

import httpx

from . import anthropic
from .auth import AI_AZURE_SCOPE, FoundryAuth
from .catalog import ModelEntry
from .config import Settings

PASSTHROUGH_HEADERS = ("retry-after", "retry-after-ms", "x-request-id", "apim-request-id")


class ChatClient:
    def __init__(self, settings: Settings, auth: FoundryAuth, http: httpx.AsyncClient | None = None):
        self.settings = settings
        self.auth = auth
        self._http = http or httpx.AsyncClient(limits=httpx.Limits(max_connections=200))
        self._owns_http = http is None

    def url_for(self, entry: ModelEntry) -> str:
        url = self.settings.url_for(entry.api)
        if not url:
            raise RuntimeError(f"no endpoint configured for {entry.api} models")
        return url

    async def send(self, entry: ModelEntry, body: dict[str, Any], *, timeout: float, stream: bool = False,
                   passthrough: bool = False) -> httpx.Response:
        """HTTP errors are returned, not raised. With stream=True the caller owns the response and must close it.

        passthrough=True (claude_translation=false): `body` is already an Anthropic Messages request; it is
        sent to Claude unchanged apart from `model`, and Claude's native response is returned unchanged.
        """
        if passthrough:
            if entry.api != "anthropic_messages":
                raise RuntimeError(f"{entry.name} cannot take an Anthropic Messages body")
            request = self._http.build_request("POST", self.url_for(entry), json=dict(body, model=entry.deployment),
                                               headers=await self._anthropic_headers(), timeout=timeout)
            return await self._http.send(request, stream=stream)
        if entry.api == "anthropic_messages":
            return await self._send_anthropic(entry, body, timeout=timeout, stream=stream)
        payload = dict(body, model=entry.deployment)
        request = self._http.build_request("POST", self.url_for(entry), json=payload, headers=await self.auth.headers(),
                                           timeout=timeout)
        return await self._http.send(request, stream=stream)

    async def _send_anthropic(self, entry: ModelEntry, body: dict[str, Any], *, timeout: float,
                              stream: bool) -> httpx.Response:
        payload = anthropic.to_messages_request(body, entry.deployment, self.settings.anthropic_default_max_tokens)
        request = self._http.build_request("POST", self.url_for(entry), json=payload,
                                           headers=await self._anthropic_headers(), timeout=timeout)
        upstream = await self._http.send(request, stream=stream)
        kept = {k: v for k, v in upstream.headers.items() if k.lower() in PASSTHROUGH_HEADERS}
        if upstream.status_code >= 400:
            content = await upstream.aread()
            await upstream.aclose()
            return httpx.Response(upstream.status_code, headers={**kept, "content-type": "application/json"},
                                  content=anthropic.error_body(content, upstream.status_code))
        if stream:
            include_usage = bool((body.get("stream_options") or {}).get("include_usage"))
            return httpx.Response(200, headers={**kept, "content-type": "text/event-stream"},
                                  stream=anthropic.ChatCompletionStream(upstream, include_usage))
        message = upstream.json()
        return httpx.Response(200, headers={**kept, "content-type": "application/json"},
                              content=json.dumps(anthropic.from_messages_response(message)).encode())

    async def _anthropic_headers(self) -> dict[str, str]:
        return {**await self.auth.headers(key_header="x-api-key", scope=AI_AZURE_SCOPE),
                "anthropic-version": self.settings.anthropic_version}

    async def send_router(self, deployment: str, body: dict[str, Any], *, timeout: float) -> httpx.Response:
        """Call a Model Router deployment (comparison harness)."""
        url = self.settings.url_for("openai_chat")
        request = self._http.build_request("POST", url, json=dict(body, model=deployment),
                                           headers=await self.auth.headers(), timeout=timeout)
        return await self._http.send(request)

    async def close(self) -> None:
        if self._owns_http:
            await self._http.aclose()
