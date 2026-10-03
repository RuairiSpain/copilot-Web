"""The value returned by ``Hack.responses`` and ``Hack.invocations``."""

from __future__ import annotations

import json
from collections.abc import AsyncIterator
from dataclasses import dataclass
from types import TracebackType
from typing import Any

from hosted_agent_kit.services.pool import PoolResult


@dataclass
class AgentResult:
    """An agent's answer, or a stream of its answer.

    Read a normal answer with ``json()``, ``text()`` or ``content``. For a stream, iterate
    ``chunks()`` (raw server-sent-event bytes) and let the iteration finish, or use ``async
    with`` or ``aclose()``: closing returns the session to the pool.
    """

    request_id: str
    status_code: int
    media_type: str
    content: bytes = b""
    replayed: bool = False
    _pool_result: PoolResult | None = None
    _stream: AsyncIterator[bytes] | None = None

    @classmethod
    def from_pool(cls, result: PoolResult) -> AgentResult:
        if result.stream is not None:
            return cls(
                request_id=result.request_id,
                status_code=result.status_code,
                media_type=result.media_type,
                _pool_result=result,
                _stream=result.stream,
            )
        if result.raw is not None:
            content = result.raw
        else:
            content = json.dumps(result.body or {}, separators=(",", ":")).encode()
        return cls(
            request_id=result.request_id,
            status_code=result.status_code,
            media_type=result.media_type,
            content=content,
        )

    @property
    def is_stream(self) -> bool:
        return self._stream is not None

    @property
    def ok(self) -> bool:
        return 200 <= self.status_code < 300

    def json(self) -> Any:
        """The body parsed as JSON. For a Responses agent this is the Responses object."""
        return json.loads(self.content)

    def text(self) -> str:
        return self.content.decode()

    def chunks(self) -> AsyncIterator[bytes]:
        """The streamed frames. Raises ``ValueError`` when the result is not a stream."""
        if self._stream is None:
            raise ValueError("this result is not a stream; read json(), text() or content")
        return self._stream

    async def aclose(self) -> None:
        """Release a stream early. Safe to call more than once and on a normal result."""
        if self._pool_result is not None:
            await self._pool_result.close()

    async def __aenter__(self) -> AgentResult:
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        await self.aclose()
