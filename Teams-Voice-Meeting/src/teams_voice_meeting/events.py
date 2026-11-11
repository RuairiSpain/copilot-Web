"""Parse Azure Event Grid deliveries from Azure Communication Services."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

SUBSCRIPTION_VALIDATION = "Microsoft.EventGrid.SubscriptionValidationEvent"
INCOMING_CALL = "Microsoft.Communication.IncomingCall"


@dataclass(frozen=True)
class IncomingCall:
    """Fields of an ACS IncomingCall event that the service needs.

    The IncomingCall payload carries ``serverCallId`` and ``correlationId``. A call connection ID
    exists only after a call is answered, and Foundry answers the call, so it is not available here.
    """

    correlation_id: str | None
    server_call_id: str | None
    caller: str | None
    callee: str | None
    caller_display_name: str | None
    incoming_call_context: str | None


def _raw_id(party: Any) -> str | None:
    if isinstance(party, dict):
        value = party.get("rawId")
        return value if isinstance(value, str) else None
    return None


def parse_incoming_call(event: dict[str, Any]) -> IncomingCall:
    data = event.get("data") or {}
    return IncomingCall(
        correlation_id=data.get("correlationId"),
        server_call_id=data.get("serverCallId"),
        caller=_raw_id(data.get("from")),
        callee=_raw_id(data.get("to")),
        caller_display_name=data.get("callerDisplayName"),
        incoming_call_context=data.get("incomingCallContext"),
    )


def validation_code(event: dict[str, Any]) -> str | None:
    if event.get("eventType") != SUBSCRIPTION_VALIDATION:
        return None
    return (event.get("data") or {}).get("validationCode")


def as_event_list(payload: Any) -> list[dict[str, Any]]:
    """Event Grid posts a JSON array. Accept a single object too."""
    if isinstance(payload, dict):
        return [payload]
    if isinstance(payload, list):
        return [e for e in payload if isinstance(e, dict)]
    return []
