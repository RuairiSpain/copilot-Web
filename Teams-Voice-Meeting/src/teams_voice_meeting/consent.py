"""ACS server consent for the Teams resource account (HMAC-signed REST call).

Without this assignment, calls to the resource account number commonly return Busy.
"""

from __future__ import annotations

import base64
import hashlib
import hmac
import json
import urllib.error
import urllib.request
from datetime import datetime, timezone
from email.utils import format_datetime

API_VERSION = "2025-06-30"


def sign_request(acs_host: str, access_key: str, path_and_query: str, body: bytes, now: datetime | None = None) -> dict[str, str]:
    now = now or datetime.now(timezone.utc)
    date = format_datetime(now, usegmt=True)
    content_hash = base64.b64encode(hashlib.sha256(body).digest()).decode()
    to_sign = f"PUT\n{path_and_query}\n{date};{acs_host};{content_hash}"
    sig = base64.b64encode(hmac.new(base64.b64decode(access_key), to_sign.encode(), hashlib.sha256).digest()).decode()
    return {
        "x-ms-date": date,
        "x-ms-content-sha256": content_hash,
        "Authorization": f"HMAC-SHA256 SignedHeaders=x-ms-date;host;x-ms-content-sha256&Signature={sig}",
        "Content-Type": "application/json",
    }


def grant_consent(acs_host: str, access_key: str, teams_tenant_id: str, resource_account_id: str) -> int:
    path = f"/access/teamsExtension/tenants/{teams_tenant_id}/assignments/{resource_account_id}?api-version={API_VERSION}"
    body = json.dumps({"principalType": "teamsResourceAccount"}).encode()
    req = urllib.request.Request(f"https://{acs_host}{path}", data=body, method="PUT",
                                 headers=sign_request(acs_host, access_key, path, body))
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.status
    except urllib.error.HTTPError as exc:
        raise RuntimeError(f"ACS consent failed: HTTP {exc.code} {exc.read()[:300]!r}") from exc
