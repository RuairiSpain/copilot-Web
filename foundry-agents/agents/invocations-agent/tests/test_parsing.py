from __future__ import annotations

import pytest
from pydantic import ValidationError

from parsing import parse_request


class FakeRequest:
    """Just enough of a Starlette request for the parser."""

    def __init__(self, payload: object) -> None:
        self._payload = payload

    async def json(self) -> object:
        return self._payload


async def test_non_streaming_wraps_the_question_in_a_list() -> None:
    run = await parse_request(FakeRequest({"question": "hi"}))  # type: ignore[arg-type]
    assert run.messages == ["hi"]
    assert run.stream is False
    assert dict(run.options) == {}


async def test_streaming_passes_a_bare_string() -> None:
    run = await parse_request(FakeRequest({"question": "hi", "stream": True}))  # type: ignore[arg-type]
    assert run.messages == "hi"
    assert run.stream is True


async def test_token_cap_becomes_a_run_option() -> None:
    run = await parse_request(FakeRequest({"question": "hi", "max_output_tokens": 64}))  # type: ignore[arg-type]
    assert dict(run.options) == {"max_tokens": 64}


@pytest.mark.parametrize(
    "payload",
    [{}, {"question": ""}, {"question": "hi", "extra": 1}, ["not", "an", "object"], None],
)
async def test_invalid_bodies_raise_value_errors(payload: object) -> None:
    # pydantic's ValidationError subclasses ValueError, which the host maps to HTTP 400.
    with pytest.raises(ValidationError) as info:
        await parse_request(FakeRequest(payload))  # type: ignore[arg-type]
    assert isinstance(info.value, ValueError)
