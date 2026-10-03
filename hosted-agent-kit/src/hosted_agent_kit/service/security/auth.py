"""Microsoft Entra ID bearer-token validation and role checks."""

from __future__ import annotations

import asyncio
import time
from collections.abc import Callable
from dataclasses import replace
from typing import Any

import httpx
import jwt
from fastapi import Request

from hosted_agent_kit.config.settings import Settings
from hosted_agent_kit.domain.errors import AuthenticationRequiredError, InsufficientScopeError
from hosted_agent_kit.service.security.claims import USER_ID_CLAIM, Principal, principal_from_claims

WWW_AUTHENTICATE = 'Bearer realm="hosted-agent-kit"'
_UNKNOWN_KID_REFRESH_SECONDS = 30.0
ALL_ROLES_DEV_USER = "dev-user"


def _challenge(error: str | None = None) -> dict[str, str]:
    value = WWW_AUTHENTICATE if error is None else f'{WWW_AUTHENTICATE}, error="{error}"'
    return {"WWW-Authenticate": value}


class JwksCache:
    """Caches the tenant signing keys. Unknown key ids trigger a rate-limited refresh."""

    def __init__(
        self,
        url: str,
        ttl_seconds: int,
        client: httpx.AsyncClient | None = None,
        monotonic: Callable[[], float] = time.monotonic,
    ) -> None:
        self._url = url
        self._ttl = ttl_seconds
        self._client = client or httpx.AsyncClient(timeout=10.0)
        self._owns_client = client is None
        self._monotonic = monotonic
        self._keys: dict[str, Any] = {}
        self._fetched_at: float | None = None
        self._refresh_lock = asyncio.Lock()

    async def close(self) -> None:
        if self._owns_client:
            await self._client.aclose()

    def _needs_refresh(self, kid: str, now: float) -> bool:
        if self._fetched_at is None or now - self._fetched_at > self._ttl:
            return True
        return kid not in self._keys and now - self._fetched_at > _UNKNOWN_KID_REFRESH_SECONDS

    async def get(self, kid: str) -> Any | None:
        if self._needs_refresh(kid, self._monotonic()):
            # One fetch at a time. Callers that waited re-check, so a burst of requests at
            # start-up or cache expiry shares a single download.
            async with self._refresh_lock:
                now = self._monotonic()
                if self._needs_refresh(kid, now):
                    await self._refresh(now)
        return self._keys.get(kid)

    async def _refresh(self, now: float) -> None:
        response = await self._client.get(self._url)
        response.raise_for_status()
        document = response.json()
        self._keys = {
            item["kid"]: jwt.PyJWK.from_dict(item).key
            for item in document.get("keys", [])
            if item.get("kty") == "RSA" and "kid" in item
        }
        self._fetched_at = now


class TokenValidator:
    def __init__(self, settings: Settings, jwks: JwksCache) -> None:
        tenant = settings.entra_tenant_id
        host = settings.entra_authority_host.rstrip("/")
        self._tenant = tenant
        self._audiences = settings.entra_audience
        self._issuers = [f"{host}/{tenant}/v2.0", f"https://sts.windows.net/{tenant}/"]
        self._leeway = settings.jwt_leeway_seconds
        self._jwks = jwks

    async def validate(self, token: str) -> dict[str, Any]:
        try:
            header = jwt.get_unverified_header(token)
            if header.get("alg") != "RS256" or not isinstance(header.get("kid"), str):
                raise jwt.InvalidTokenError("unsupported header")
            key = await self._jwks.get(header["kid"])
            if key is None:
                raise jwt.InvalidTokenError("unknown signing key")
            claims: dict[str, Any] = jwt.decode(
                token,
                key,
                algorithms=["RS256"],
                audience=self._audiences,
                issuer=self._issuers,
                leeway=self._leeway,
                options={"require": ["exp", "iss", "aud"]},
            )
        except (jwt.InvalidTokenError, httpx.HTTPError, ValueError) as exc:
            raise AuthenticationRequiredError(
                "The bearer token is invalid or has expired.", headers=_challenge("invalid_token")
            ) from exc
        if claims.get("tid") != self._tenant:
            raise AuthenticationRequiredError(
                "The bearer token was issued for a different tenant.",
                headers=_challenge("invalid_token"),
            )
        return claims


class Authenticator:
    """Resolves the caller for a request according to the configured auth mode."""

    def __init__(self, settings: Settings, validator: TokenValidator | None) -> None:
        self._settings = settings
        self._validator = validator

    async def authenticate(self, request: Request) -> Principal:
        if self._validator is None:
            return self._development_principal(request)
        header = request.headers.get("authorization", "")
        scheme, _, token = header.partition(" ")
        if scheme.lower() != "bearer" or not token.strip():
            raise AuthenticationRequiredError("A bearer token is required.", headers=_challenge())
        claims = await self._validator.validate(token.strip())
        try:
            return principal_from_claims(claims)
        except AuthenticationRequiredError as exc:
            exc.headers = _challenge("invalid_token")
            raise

    def _development_principal(self, request: Request) -> Principal:
        s = self._settings
        user = request.headers.get("x-dev-user-id", ALL_ROLES_DEV_USER)
        roles = [s.invoke_role, s.admin_role, s.diagnostics_role]
        claims = {USER_ID_CLAIM: user, "roles": roles}
        return replace(principal_from_claims(claims), app_only=False)

    async def authorise(self, request: Request, role: str) -> Principal:
        principal = await self.authenticate(request)
        if not principal.has(role):
            raise InsufficientScopeError("The caller lacks the required role.")
        return principal
