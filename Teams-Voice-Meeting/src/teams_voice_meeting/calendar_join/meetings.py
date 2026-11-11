"""Turn calendar events into joinable Teams meetings, and Teams join URLs into Graph meeting info."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any, Iterable
from urllib.parse import unquote, urlsplit

TEAMS_PROVIDER = "teamsForBusiness"
_LONG = re.compile(r"https://teams\.microsoft\.com.*/(?P<thread>[^/]+)/(?P<message>[^/]+)\?context=(?P<context>\{.*\})")
_SHORT_PATH = re.compile(r"^/meet/(?P<id>\d+)$")
_PASSCODE = re.compile(r"(?:^|&)p=(?P<p>[^&]*)")


def parse_graph_time(value: str) -> datetime:
    """Graph returns e.g. '2026-11-12T10:00:00.0000000' (UTC when asked with Prefer: outlook.timezone)."""
    value = re.sub(r"(\.\d{6})\d+", r"\1", value.rstrip("Z"))
    return datetime.fromisoformat(value).replace(tzinfo=timezone.utc)


@dataclass(frozen=True)
class Meeting:
    key: str          # event id + start, unique per occurrence
    subject: str
    start: datetime
    end: datetime
    join_url: str


def meetings_from_events(events: Iterable[dict[str, Any]]) -> list[Meeting]:
    out = []
    for e in events:
        if e.get("isCancelled") or not e.get("isOnlineMeeting"):
            continue
        if e.get("onlineMeetingProvider") not in (None, TEAMS_PROVIDER):
            continue
        if (e.get("responseStatus") or {}).get("response") == "declined":
            continue
        url = (e.get("onlineMeeting") or {}).get("joinUrl")
        if not url:
            continue
        start = parse_graph_time(e["start"]["dateTime"])
        out.append(Meeting(f"{e['id']}|{start.isoformat()}", e.get("subject") or "", start,
                           parse_graph_time(e["end"]["dateTime"]), url))
    return sorted(out, key=lambda m: m.start)


@dataclass(frozen=True)
class JoinTarget:
    tenant_id: str | None
    chat_info: dict | None
    meeting_info: dict


def parse_join_url(url: str, tenant_id: str | None = None) -> JoinTarget:
    """Same two URL forms the Microsoft EchoBot sample accepts.

    Short ``/meet/<digits>?p=<passcode>``: joinMeetingIdMeetingInfo, needs ``tenant_id``.
    Long ``/l/meetup-join/<thread>/<message>?context={...}``: chatInfo and organizerMeetingInfo.
    """
    parts = urlsplit(url)
    short = _SHORT_PATH.match(parts.path)
    if short:
        if not tenant_id:
            raise ValueError("A short Teams join URL needs a tenant ID (GRAPH_TENANT_ID)")
        passcode = _PASSCODE.search(parts.query)  # raw: decoding would corrupt '+' and '%26'
        return JoinTarget(tenant_id, None, {
            "@odata.type": "#microsoft.graph.joinMeetingIdMeetingInfo",
            "joinMeetingId": short["id"],
            "passcode": passcode["p"] if passcode else None,
        })
    m = _LONG.match(unquote(url))
    if not m:
        raise ValueError(f"Unrecognised Teams join URL: {url[:80]}")
    ctx = json.loads(m["context"])
    if not ctx.get("Tid") or not ctx.get("Oid"):
        raise ValueError("Join URL context is missing Tid or Oid")
    chat = {"@odata.type": "#microsoft.graph.chatInfo", "threadId": m["thread"], "messageId": m["message"]}
    if ctx.get("MessageId"):
        chat["replyChainMessageId"] = ctx["MessageId"]
    return JoinTarget(ctx["Tid"], chat, {
        "@odata.type": "#microsoft.graph.organizerMeetingInfo",
        "organizer": {"@odata.type": "#microsoft.graph.identitySet",
                      "user": {"@odata.type": "#microsoft.graph.identity", "id": ctx["Oid"], "tenantId": ctx["Tid"]}},
    })
