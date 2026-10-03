"""Turns an HTTP request into a run of the agent."""

from __future__ import annotations

from agent_framework_foundry_hosting import InvocationRun
from starlette.requests import Request

from schemas import InvokeRequest


async def parse_request(request: Request) -> InvocationRun:
    """Validate the JSON body and convert it to an ``InvocationRun``.

    Malformed JSON and schema violations raise ``ValueError`` (``pydantic.ValidationError``
    is a subclass), which the host turns into an HTTP 400 response.
    """
    body = InvokeRequest.model_validate(await request.json())

    options: dict[str, int] = {}
    if body.max_output_tokens is not None:
        options["max_tokens"] = body.max_output_tokens

    # A bare string for streaming and a one-item list otherwise mirrors the host's own
    # default parser, so both paths behave exactly as the platform expects.
    messages = body.question if body.stream else [body.question]
    return InvocationRun(messages=messages, options=options, stream=body.stream)
