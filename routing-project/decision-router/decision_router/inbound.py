"""Who may call the router: an API key, a Microsoft Entra ID access token, or either.

A request is accepted when it carries any one valid credential:

- **API key** (`ROUTER_API_KEYS`, comma-separated so keys can be rotated) in the `api-key` or
  `x-api-key` header, or as `Authorization: Bearer <key>`. Foundry Agent Service model
  connections send a key in whichever header and scheme the connection is configured with.
- **Entra ID token** (`ROUTER_ENTRA_TENANT_ID` + `ROUTER_ENTRA_AUDIENCE`) as
  `Authorization: Bearer <jwt>`. This covers managed identities (a Foundry project's identity
  calling through API Management, an Azure workload) and OAuth 2.0 client credentials (the
  "OAuth 2.0" option on an Agent Service model connection). The token's signature is checked
  against the tenant's published keys, and its issuer, audience and expiry are checked.
  `ROUTER_ENTRA_ALLOWED_CLIENT_IDS` limits which applications or managed identities (the
  token's `azp`/`appid`) may call; leave it empty to accept any client the tenant issued a
  token to for this audience.

With neither configured the router refuses to start unless ROUTER_ALLOW_UNAUTHENTICATED=true.
Health and metrics endpoints are never authenticated.
"""
from __future__ import annotations

import asyncio
import hmac
import time
from dataclasses import dataclass
from typing import Any

import httpx

ENTRA_AUTHORITY = "https://login.microsoftonline.com"
JWKS_TTL_SECONDS = 24 * 3600
JWKS_MIN_REFRESH_SECONDS = 300  # an unknown signing key triggers at most one refetch per 5 minutes


class Unauthorized(Exception):
    def __init__(self, message: str):
        super().__init__(message)
        self.message = message


@dataclass(frozen=True)
class Caller:
    method: str  # "api_key", "entra" or "none"
    identity: str | None = None  # key number (1-based) or the token's client ID

    def log(self) -> dict[str, Any]:
        return {"method": self.method, "identity": self.identity}


class InboundAuth:
    def __init__(self, api_keys: tuple[str, ...] = (), tenant_id: str | None = None,
                 audiences: tuple[str, ...] = (), allowed_client_ids: tuple[str, ...] = (),
                 allow_unauthenticated: bool = False, http: httpx.AsyncClient | None = None,
                 clock=time.time):
        if bool(tenant_id) != bool(audiences):
            raise ValueError("set both ROUTER_ENTRA_TENANT_ID and ROUTER_ENTRA_AUDIENCE, or neither")
        self.api_keys = tuple(k for k in api_keys if k)
        self.tenant_id = tenant_id
        self.audiences = audiences
        self.allowed_client_ids = {c.lower() for c in allowed_client_ids}
        self.allow_unauthenticated = allow_unauthenticated
        if not self.enabled and not allow_unauthenticated:
            raise ValueError("no inbound authentication configured: set ROUTER_API_KEYS and/or "
                             "ROUTER_ENTRA_TENANT_ID + ROUTER_ENTRA_AUDIENCE, or ROUTER_ALLOW_UNAUTHENTICATED=true")
        self._http = http
        self._clock = clock
        self._keys: dict[str, Any] = {}
        self._fetched_at = 0.0
        self._lock = asyncio.Lock()

    @property
    def enabled(self) -> bool:
        return bool(self.api_keys or self.tenant_id)

    @property
    def methods(self) -> list[str]:
        return [m for m, on in (("api_key", self.api_keys), ("entra", self.tenant_id)) if on]

    async def check(self, headers: Any) -> Caller:
        if not self.enabled:
            return Caller("none")
        bearer = None
        auth = headers.get("authorization") or ""
        if auth[:7].lower() == "bearer ":
            bearer = auth[7:].strip()
        candidates = [headers.get("api-key"), headers.get("x-api-key"), bearer]
        for presented in filter(None, candidates):
            index = self._match_key(presented)
            if index is not None:
                return Caller("api_key", str(index))
        if bearer and self.tenant_id and bearer.count(".") == 2:
            return Caller("entra", await self._verify_token(bearer))
        if not any(candidates):
            raise Unauthorized(f"missing credentials; accepted: {', '.join(self.methods)}")
        raise Unauthorized("invalid credentials")

    def _match_key(self, presented: str) -> int | None:
        found = None
        for i, key in enumerate(self.api_keys, start=1):
            if hmac.compare_digest(presented.encode(), key.encode()):  # compare all keys: constant time
                found = i
        return found

    # ---------------------------------------------------------------- Entra ID tokens
    @property
    def issuers(self) -> list[str]:
        # v2.0 tokens and v1.0 tokens (the version depends on the app registration)
        return [f"{ENTRA_AUTHORITY}/{self.tenant_id}/v2.0", f"https://sts.windows.net/{self.tenant_id}/"]

    async def _verify_token(self, token: str) -> str:
        import jwt  # PyJWT

        try:
            header = jwt.get_unverified_header(token)
        except jwt.PyJWTError as exc:
            raise Unauthorized("malformed bearer token") from exc
        key = await self._signing_key(header.get("kid"))
        try:
            claims = jwt.decode(token, key, algorithms=["RS256"], audience=list(self.audiences),
                                issuer=self.issuers, options={"require": ["exp", "iss", "aud"]}, leeway=60)
        except jwt.PyJWTError as exc:
            raise Unauthorized(f"token rejected: {exc}") from exc
        if claims.get("tid") and claims["tid"] != self.tenant_id:
            raise Unauthorized("token is from another tenant")
        client = str(claims.get("azp") or claims.get("appid") or "")
        if self.allowed_client_ids and client.lower() not in self.allowed_client_ids:
            raise Unauthorized(f"client {client or '(none)'} is not in ROUTER_ENTRA_ALLOWED_CLIENT_IDS")
        return client or str(claims.get("oid") or "")

    async def _signing_key(self, kid: str | None) -> Any:
        if not kid:
            raise Unauthorized("token has no key id")
        now = self._clock()
        stale = now - self._fetched_at > JWKS_TTL_SECONDS
        if stale or (kid not in self._keys and now - self._fetched_at > JWKS_MIN_REFRESH_SECONDS):
            async with self._lock:
                if self._fetched_at == 0.0 or self._clock() - self._fetched_at > JWKS_MIN_REFRESH_SECONDS:
                    await self._fetch_keys()
        if kid not in self._keys:
            raise Unauthorized("token signed with an unknown key")
        return self._keys[kid]

    async def _fetch_keys(self) -> None:
        import jwt

        url = f"{ENTRA_AUTHORITY}/{self.tenant_id}/discovery/v2.0/keys"
        http = self._http or httpx.AsyncClient()
        try:
            response = await http.get(url, timeout=10)
            response.raise_for_status()
            keys = {}
            for jwk in response.json().get("keys", []):
                if jwk.get("kty") == "RSA" and jwk.get("kid"):
                    keys[jwk["kid"]] = jwt.PyJWK(jwk, algorithm="RS256").key
        except (httpx.HTTPError, ValueError, jwt.PyJWTError) as exc:
            if not self._keys:
                raise Unauthorized("cannot fetch Entra ID signing keys") from exc
            return  # keep the keys we have
        finally:
            if self._http is None:
                await http.aclose()
        self._keys, self._fetched_at = keys, self._clock()
