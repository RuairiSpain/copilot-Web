"""Minimal Microsoft Graph client (standard library HTTP + azure-identity)."""

from __future__ import annotations

import json
import urllib.error
import urllib.request
from datetime import datetime, timezone
from typing import Any, Iterator
from urllib.parse import quote

GRAPH = "https://graph.microsoft.com/v1.0"
SCOPE = "https://graph.microsoft.com/.default"
EVENT_FIELDS = "id,subject,start,end,isCancelled,isOnlineMeeting,onlineMeetingProvider,onlineMeeting,responseStatus"


class GraphError(RuntimeError):
    pass


def iso_utc(dt: datetime) -> str:
    return dt.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


class GraphClient:
    """App-only access. Locally set AZURE_TENANT_ID, AZURE_CLIENT_ID and AZURE_CLIENT_SECRET;
    in Azure use a managed identity. ``az login`` tokens do not carry the app permissions."""

    def __init__(self, credential: Any | None = None, opener=urllib.request.urlopen) -> None:
        if credential is None:
            from azure.identity import DefaultAzureCredential
            credential = DefaultAzureCredential()
        self._credential = credential
        self._open = opener

    def request(self, method: str, url: str, body: dict | None = None, headers: dict | None = None) -> dict:
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(url, data=data, method=method, headers={
            "Authorization": f"Bearer {self._credential.get_token(SCOPE).token}",
            "Content-Type": "application/json",
            **(headers or {}),
        })
        try:
            with self._open(req, timeout=30) as resp:
                raw = resp.read()
        except urllib.error.HTTPError as exc:
            raise GraphError(f"{method} {url} -> {exc.code}: {exc.read()[:500]!r}") from exc
        return json.loads(raw) if raw else {}

    def calendar_view(self, mailbox: str, start: datetime, end: datetime) -> Iterator[dict]:
        """Events that overlap [start, end], with times returned in UTC. Follows paging."""
        url = (f"{GRAPH}/users/{quote(mailbox)}/calendarView?startDateTime={iso_utc(start)}"
               f"&endDateTime={iso_utc(end)}&$select={EVENT_FIELDS}&$top=50")
        while url:
            page = self.request("GET", url, headers={"Prefer": 'outlook.timezone="UTC"'})
            yield from page.get("value", [])
            url = page.get("@odata.nextLink")

    def join_call(self, body: dict) -> dict:
        return self.request("POST", f"{GRAPH}/communications/calls", body)

    def delete_call(self, call_id: str) -> None:
        self.request("DELETE", f"{GRAPH}/communications/calls/{quote(call_id)}")
