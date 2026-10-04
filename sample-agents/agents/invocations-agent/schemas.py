"""The public JSON contract of the Invocations endpoint, as Pydantic models.

Owning the contract is the reason to choose the Invocations protocol. Everything a client
can send is validated here, and the same models generate the OpenAPI document.
"""

from __future__ import annotations

from typing import Any

from pydantic import BaseModel, ConfigDict, Field

MAX_QUESTION_CHARS = 8_000
MIN_OUTPUT_TOKENS = 16
MAX_OUTPUT_TOKENS = 4_096


class InvokeRequest(BaseModel):
    """Body of ``POST /invocations``."""

    model_config = ConfigDict(extra="forbid", str_strip_whitespace=True)

    question: str = Field(
        min_length=1,
        max_length=MAX_QUESTION_CHARS,
        description="The question to answer.",
        examples=["How do I rotate an API key?"],
    )
    stream: bool = Field(
        default=False,
        description="Return server-sent events (delta frames, then done) instead of JSON.",
    )
    max_output_tokens: int | None = Field(
        default=None,
        ge=MIN_OUTPUT_TOKENS,
        le=MAX_OUTPUT_TOKENS,
        description="Optional cap on the length of the answer.",
    )


class InvokeResponse(BaseModel):
    """Non-streaming response body."""

    response: str = Field(description="The agent's answer.")


class ErrorResponse(BaseModel):
    """Error body returned for invalid requests."""

    error: str


def build_openapi_spec() -> dict[str, Any]:
    """OpenAPI 3.1 description of the endpoint, generated from the models above."""
    request_schema = InvokeRequest.model_json_schema()
    return {
        "openapi": "3.1.0",
        "info": {
            "title": "invocations-agent",
            "version": "1.0.0",
            "description": "Typed question-answering API over AI Search and a code interpreter.",
        },
        "paths": {
            "/invocations": {
                "post": {
                    "operationId": "invoke",
                    "summary": "Ask a question",
                    "requestBody": {
                        "required": True,
                        "content": {"application/json": {"schema": request_schema}},
                    },
                    "responses": {
                        "200": {
                            "description": "The answer. Server-sent events when stream is true.",
                            "content": {
                                "application/json": {"schema": InvokeResponse.model_json_schema()},
                                "text/event-stream": {"schema": {"type": "string"}},
                            },
                        },
                        "400": {
                            "description": "The request body is not valid.",
                            "content": {
                                "application/json": {"schema": ErrorResponse.model_json_schema()}
                            },
                        },
                    },
                }
            }
        },
    }
