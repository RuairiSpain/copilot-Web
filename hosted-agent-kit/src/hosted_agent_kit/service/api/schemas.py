"""Request and response models for the public and admin APIs."""

from __future__ import annotations

from typing import Annotated, Any, Self

from pydantic import BaseModel, ConfigDict, Field, StringConstraints, model_validator


class _Strict(BaseModel):
    model_config = ConfigDict(extra="forbid")


CONVERSATION_KEY_PATTERN = r"^[A-Za-z0-9._:-]{1,128}$"
ConversationKey = Annotated[str, StringConstraints(pattern=CONVERSATION_KEY_PATTERN)]
_CONVERSATION_DOC = (
    "Opaque key for one of the caller's conversations with a stateful agent. Each key gets its "
    "own session, scoped under the authenticated user. Also accepted as `X-Conversation-Key`."
)


class InvokeRequest(_Strict):
    input: dict[str, Any]
    conversation_key: ConversationKey | None = Field(default=None, description=_CONVERSATION_DOC)
    timeout_seconds: float | None = Field(default=None, gt=0)
    user_id: str | None = Field(
        default=None,
        description="Accepted only when POOL_AUTH_MODE=development; rejected otherwise.",
    )


class ResponsesRequest(_Strict):
    """A Responses-protocol call. ``input`` is the Responses payload."""

    input: dict[str, Any]
    stream: bool = Field(default=False, description="Return server-sent events.")
    conversation_key: ConversationKey | None = Field(default=None, description=_CONVERSATION_DOC)
    timeout_seconds: float | None = Field(default=None, gt=0)
    user_id: str | None = Field(
        default=None,
        description="Accepted only when POOL_AUTH_MODE=development; rejected otherwise.",
    )


class ChatRequest(_Strict):
    message: str = Field(min_length=1)
    conversation_key: ConversationKey | None = Field(default=None, description=_CONVERSATION_DOC)
    previous_response_id: str | None = Field(default=None, min_length=1)
    conversation_id: str | None = Field(default=None, min_length=1)
    metadata: dict[str, str] = Field(default_factory=dict)
    timeout_seconds: float | None = Field(default=None, gt=0)
    stream: bool = False
    user_id: str | None = Field(
        default=None,
        description="Accepted only when POOL_AUTH_MODE=development; rejected otherwise.",
    )

    @model_validator(mode="after")
    def _one_thread_reference(self) -> Self:
        if self.previous_response_id and self.conversation_id:
            raise ValueError("provide previous_response_id or conversation_id, not both")
        return self


class InvokeResponse(BaseModel):
    request_id: str
    result: dict[str, Any]


class InvocationEnvelope(BaseModel):
    """An Invocations response wrapped for clients that want one shape. Exactly one body is set."""

    request_id: str
    status_code: int
    content_type: str
    body_json: Any | None = Field(default=None, description="The body, when it is JSON.")
    body_text: str | None = Field(default=None, description="The body, when it is text.")
    body_base64: str | None = Field(default=None, description="The body, for any other content.")


class Limits(BaseModel):
    max_body_bytes: int
    default_timeout_seconds: float
    max_timeout_seconds: float
    max_stream_seconds: float
    max_request_seconds: float | None
    max_response_bytes: int | None = Field(
        description="Largest non-streaming response (Invocations agents)."
    )


class AgentCapabilities(BaseModel):
    """What a caller can do with one agent. Contains no administrative detail."""

    name: str
    protocol: str = Field(description="responses or invocations.")
    stateful: bool = Field(description="True when each user (and conversation) keeps a session.")
    streaming: str = Field(
        description="`request`: the caller asks with `stream`. `agent`: the agent decides."
    )
    supports_conversation_key: bool
    endpoints: list[str]
    request_content_types: list[str]
    queue_enabled: bool
    queue_max_wait_seconds: float | None
    idempotency_ttl_seconds: float = Field(
        description="How long a repeated Idempotency-Key replays the stored response. 0 is off."
    )
    agent_version: str | None = Field(description="Pinned version for new sessions, or null.")
    limits: Limits


class ProblemDetails(BaseModel):
    type: str
    title: str
    status: int
    detail: str
    error_code: str
    correlation_id: str | None = None
    request_id: str | None = None
    phase: str = Field(
        description="Where the request failed: request, auth, queue, create, invoke or stream."
    )
    retry_safe: bool = Field(
        description="True when retrying the same request cannot repeat work the agent already did."
    )
    retry_after_seconds: int | None = None
