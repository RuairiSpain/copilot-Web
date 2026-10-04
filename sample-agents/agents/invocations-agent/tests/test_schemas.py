from __future__ import annotations

import pytest
from pydantic import ValidationError

from schemas import (
    MAX_OUTPUT_TOKENS,
    MAX_QUESTION_CHARS,
    MIN_OUTPUT_TOKENS,
    InvokeRequest,
    build_openapi_spec,
)


def test_minimal_request_uses_defaults() -> None:
    request = InvokeRequest(question="What is X?")
    assert request.stream is False
    assert request.max_output_tokens is None


def test_whitespace_is_stripped() -> None:
    assert InvokeRequest(question="  hello \n").question == "hello"


@pytest.mark.parametrize("question", ["", "   ", "x" * (MAX_QUESTION_CHARS + 1)])
def test_question_length_is_bounded(question: str) -> None:
    with pytest.raises(ValidationError):
        InvokeRequest(question=question)


def test_unknown_fields_are_rejected() -> None:
    with pytest.raises(ValidationError, match="extra"):
        InvokeRequest.model_validate({"question": "hi", "temperature": 2})


@pytest.mark.parametrize("tokens", [MIN_OUTPUT_TOKENS - 1, MAX_OUTPUT_TOKENS + 1, 0, -5])
def test_token_cap_is_bounded(tokens: int) -> None:
    with pytest.raises(ValidationError):
        InvokeRequest(question="hi", max_output_tokens=tokens)


def test_stream_must_be_a_boolean() -> None:
    with pytest.raises(ValidationError):
        InvokeRequest.model_validate({"question": "hi", "stream": "maybe"})


class TestOpenApi:
    def test_describes_the_invocations_endpoint(self) -> None:
        spec = build_openapi_spec()
        assert spec["openapi"].startswith("3.1")
        operation = spec["paths"]["/invocations"]["post"]
        assert set(operation["responses"]) == {"200", "400"}

    def test_request_schema_forbids_extra_properties(self) -> None:
        spec = build_openapi_spec()
        schema = spec["paths"]["/invocations"]["post"]["requestBody"]["content"][
            "application/json"
        ]["schema"]
        assert schema["additionalProperties"] is False
        assert schema["required"] == ["question"]

    def test_both_response_media_types_are_documented(self) -> None:
        spec = build_openapi_spec()
        content = spec["paths"]["/invocations"]["post"]["responses"]["200"]["content"]
        assert set(content) == {"application/json", "text/event-stream"}
