"""OpenAI-compatible chat completions against Foundry deployments (the v1 route)."""
from __future__ import annotations

from typing import Any

import httpx

from .auth import FoundryAuth


class ChatClient:
    def __init__(self, url: str, auth: FoundryAuth, http: httpx.AsyncClient | None = None):
        self.url = url
        self.auth = auth
        self._http = http or httpx.AsyncClient(limits=httpx.Limits(max_connections=200))
        self._owns_http = http is None

    async def send(self, deployment: str, body: dict[str, Any], *, timeout: float, stream: bool = False) -> httpx.Response:
        """POST the client's body with `model` set to the deployment. HTTP errors are returned, not raised.

        With stream=True the caller owns the response and must close it.
        """
        payload = dict(body, model=deployment)
        request = self._http.build_request("POST", self.url, json=payload, headers=await self.auth.headers(),
                                           timeout=timeout)
        return await self._http.send(request, stream=stream)

    async def close(self) -> None:
        if self._owns_http:
            await self._http.aclose()
