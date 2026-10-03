"""A small server-sent-events parser."""

from __future__ import annotations

import json
from collections.abc import AsyncIterable, AsyncIterator
from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True)
class SseEvent:
    event: str
    data: Any  # parsed JSON when the data is JSON, otherwise the text
    raw: str


def _decode(raw: str) -> Any:
    try:
        return json.loads(raw)
    except ValueError:
        return raw


async def parse_sse(chunks: AsyncIterable[str]) -> AsyncIterator[SseEvent]:
    """Yield events from decoded text chunks. Chunks may split lines and events anywhere."""
    buffer = ""
    event = "message"
    data: list[str] = []

    def feed(line: str) -> SseEvent | None:
        nonlocal event, data
        line = line.removesuffix("\r")
        if line == "":
            finished = None
            if data:
                raw = "\n".join(data)
                finished = SseEvent(event, _decode(raw), raw)
            event, data = "message", []
            return finished
        if line.startswith(":"):
            return None  # a comment
        name, _, value = line.partition(":")
        value = value.removeprefix(" ")
        if name == "event":
            event = value
        elif name == "data":
            data.append(value)
        return None

    async for chunk in chunks:
        buffer += chunk
        while (newline := buffer.find("\n")) >= 0:
            line, buffer = buffer[:newline], buffer[newline + 1 :]
            if (finished := feed(line)) is not None:
                yield finished
    # The stream ended without a final newline or blank line.
    if buffer and (finished := feed(buffer)) is not None:
        yield finished
    if (finished := feed("")) is not None:
        yield finished
