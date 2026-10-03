"""A canned Foundry backend served through the real SDK pipelines (no network)."""

from __future__ import annotations

import asyncio
import json
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime
from types import SimpleNamespace
from typing import Any

import httpx2
from azure.ai.projects.aio import AIProjectClient
from azure.core.credentials import AccessToken
from azure.core.pipeline.transport import AsyncHttpTransport
from azure.core.rest._http_response_impl_async import AsyncHttpResponseImpl
from azure.core.utils import CaseInsensitiveDict

from hosted_agent_kit.adapters.foundry_sdk import CLIENT_OPTIONS, SdkFoundryAdapter

ENDPOINT = "https://acct.services.ai.azure.com/api/projects/proj"
SESSION_JSON = {
    "agent_session_id": "sess-1",
    "version_indicator": {"type": "version_ref", "agent_version": "7"},
    "status": "active",
    "created_at": 1_790_000_000,
    "last_accessed_at": 1_790_000_100,
    "expires_at": 1_790_100_000,
}


@dataclass
class Recorded:
    method: str
    path: str
    query: dict[str, str]
    body: Any
    headers: dict[str, str]


@dataclass
class Backend:
    requests: list[Recorded] = field(default_factory=list)
    delay: float = 0.0
    routes: dict[tuple[str, str], Callable[[Recorded], tuple[int, Any, dict[str, str]]]] = field(
        default_factory=dict
    )

    def route(
        self,
        method: str,
        path_suffix: str,
        status: int,
        body: Any = None,
        headers: dict[str, str] | None = None,
    ) -> None:
        self.routes[(method, path_suffix)] = lambda _: (status, body, headers or {})

    def handle(self, recorded: Recorded) -> tuple[int, Any, dict[str, str]]:
        self.requests.append(recorded)
        for (method, suffix), handler in self.routes.items():
            if method == recorded.method and recorded.path.endswith(suffix):
                return handler(recorded)
        return 404, {"error": {"code": "not_found", "message": "no route"}}, {}


class Credential:
    async def get_token(self, *scopes: str, **kwargs: Any) -> AccessToken:
        return AccessToken("test-token", int(datetime.now(UTC).timestamp()) + 3600)

    async def __aenter__(self) -> Credential:
        return self

    async def __aexit__(self, *args: Any) -> None:
        return None

    async def close(self) -> None:
        return None


class BackendTransport(AsyncHttpTransport):  # type: ignore[type-arg]
    def __init__(self, backend: Backend) -> None:
        self._backend = backend

    async def open(self) -> None:
        return None

    async def close(self) -> None:
        return None

    async def __aenter__(self) -> BackendTransport:
        return self

    async def __aexit__(self, *args: Any) -> None:
        return None

    async def send(self, request: Any, **kwargs: Any) -> Any:
        url = httpx2.URL(request.url)
        body = json.loads(request.body) if request.body else None
        recorded = Recorded(request.method, url.path, dict(url.params), body, dict(request.headers))
        status, payload, headers = self._backend.handle(recorded)
        if self._backend.delay:
            await asyncio.sleep(self._backend.delay)
        if isinstance(payload, bytes):
            content = payload
        else:
            content = json.dumps(payload).encode() if payload is not None else b""
        response = AsyncHttpResponseImpl(
            request=request,
            internal_response=SimpleNamespace(close=lambda: None),
            block_size=4096,
            status_code=status,
            reason="x",
            content_type="application/json",
            headers=CaseInsensitiveDict({"Content-Type": "application/json", **headers}),
            stream_download_generator=None,
        )
        # Pre-load the body so the SDK treats the response as fully read.
        response._content = content
        response._is_stream_consumed = True
        response._is_closed = True
        return response


def build_adapter(
    backend: Backend, isolation_key: str | None = None, max_response_bytes: int | None = None
) -> SdkFoundryAdapter:
    def factory() -> AIProjectClient:
        project = AIProjectClient(
            endpoint=ENDPOINT,
            credential=Credential(),
            transport=BackendTransport(backend),
            **CLIENT_OPTIONS,  # type: ignore[arg-type]
        )
        original = project.get_openai_client

        def with_mock_http(**kwargs: Any) -> Any:
            def handler(request: httpx2.Request) -> httpx2.Response:
                body = json.loads(request.content) if request.content else None
                recorded = Recorded(
                    request.method,
                    request.url.path,
                    dict(request.url.params),
                    body,
                    dict(request.headers),
                )
                status, payload, headers = backend.handle(recorded)
                if isinstance(payload, bytes):
                    return httpx2.Response(status, content=payload, headers=headers)
                return httpx2.Response(status, json=payload, headers=headers)

            client = httpx2.AsyncClient(transport=httpx2.MockTransport(handler))
            return original(http_client=client, **kwargs)

        project.get_openai_client = with_mock_http  # type: ignore[method-assign,assignment]
        return project

    return SdkFoundryAdapter(
        ENDPOINT,
        client_factory=factory,
        isolation_key=isolation_key,
        max_response_bytes=max_response_bytes,
    )
