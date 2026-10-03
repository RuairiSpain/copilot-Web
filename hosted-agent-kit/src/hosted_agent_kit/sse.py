"""Parse server-sent events from the byte chunks a streamed result yields."""

from __future__ import annotations

import json
from collections.abc import AsyncIterator
from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True)
class SseEvent:
    """One event. ``data`` is the joined ``data:`` lines; ``json()`` parses it."""

    event: str = "message"
    data: str = ""
    id: str | None = None
    retry: int | None = None

    def json(self) -> Any:
        return json.loads(self.data)

    @property
    def is_error(self) -> bool:
        """A failure after the stream started arrives as a final ``event: error`` frame."""
        return self.event == "error"


class _Parser:
    def __init__(self) -> None:
        self._buffer = b""
        self._event = "message"
        self._data: list[str] = []
        self._id: str | None = None
        self._retry: int | None = None

    def feed(self, chunk: bytes) -> list[SseEvent]:
        self._buffer += chunk
        events: list[SseEvent] = []
        while True:
            line, found = self._next_line()
            if not found:
                return events
            event = self._line(line)
            if event is not None:
                events.append(event)

    def _next_line(self) -> tuple[str, bool]:
        for terminator in (b"\r\n", b"\n", b"\r"):
            index = self._buffer.find(terminator)
            if index < 0:
                continue
            # A lone \r at the very end may be the start of \r\n: wait for the next chunk.
            if terminator == b"\r" and index == len(self._buffer) - 1:
                return "", False
            line = self._buffer[:index].decode("utf-8", errors="replace")
            self._buffer = self._buffer[index + len(terminator) :]
            return line, True
        return "", False

    def _line(self, line: str) -> SseEvent | None:
        if line == "":
            return self._dispatch()
        if line.startswith(":"):
            return None  # a comment, often a keep-alive
        name, _, value = line.partition(":")
        value = value.removeprefix(" ")
        if name == "event":
            self._event = value
        elif name == "data":
            self._data.append(value)
        elif name == "id":
            self._id = value
        elif name == "retry" and value.isdigit():
            self._retry = int(value)
        return None

    def _dispatch(self) -> SseEvent | None:
        if not self._data and self._event == "message":
            return None
        event = SseEvent(self._event, "\n".join(self._data), self._id, self._retry)
        self._event, self._data, self._retry = "message", [], None
        return event

    def finish(self) -> SseEvent | None:
        """An event that ended without its blank line, at the end of the stream."""
        return self._dispatch() if self._data else None


async def parse_sse(chunks: AsyncIterator[bytes]) -> AsyncIterator[SseEvent]:
    """Yield events as they complete, whatever the chunk boundaries."""
    parser = _Parser()
    async for chunk in chunks:
        for event in parser.feed(chunk):
            yield event
    tail = parser.finish()
    if tail is not None:
        yield tail
