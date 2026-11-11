"""Join a Teams meeting with the Graph cloud communications API."""

from __future__ import annotations

from .graph import GraphClient
from .meetings import JoinTarget, parse_join_url


def build_join_request(target: JoinTarget, callback_uri: str) -> dict:
    """POST /communications/calls body. Service-hosted media: the bot joins the roster, but there is
    no live audio stream to a voice agent. That needs app-hosted media (see README)."""
    body: dict = {
        "@odata.type": "#microsoft.graph.call",
        "callbackUri": callback_uri,
        "requestedModalities": ["audio"],
        "mediaConfig": {"@odata.type": "#microsoft.graph.serviceHostedMediaConfig"},
        "meetingInfo": target.meeting_info,
    }
    if target.chat_info:
        body["chatInfo"] = target.chat_info
    if target.tenant_id:
        body["tenantId"] = target.tenant_id
    return body


def join(graph: GraphClient, join_url: str, callback_uri: str, tenant_id: str | None = None) -> str:
    """Join and return the Graph call ID."""
    target = parse_join_url(join_url, tenant_id)
    return graph.join_call(build_join_request(target, callback_uri))["id"]
