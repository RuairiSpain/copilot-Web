"""Sign-in for your own endpoints: validate a Microsoft Entra bearer token, return who called.

The kit trusts the ``user_id`` you give it. For a stateful agent that id selects whose session is
used, so take it from a validated token and never from the request body or a header the caller
controls. ``EntraAuth`` does the validation (signature, issuer, audience, tenant, expiry) and
gives you FastAPI dependencies::

    auth = EntraAuth(tenant_id="...", audience="api://my-app")
    install(app, kit)
    auth.install(app)

    @app.post("/ask")
    async def ask(body: Ask, kit: KitDep, user: str = Depends(auth.user)):
        return (await kit.ask("support-bot", body.text, user_id=user)).json()

    app.include_router(admin_router(), dependencies=[Depends(auth.require_role("Pool.Admin"))])

Needs the ``service`` extra (httpx and PyJWT).
"""

from __future__ import annotations

from collections.abc import AsyncIterator, Awaitable, Callable, Sequence
from contextlib import AsyncExitStack, asynccontextmanager
from typing import Any

from fastapi import FastAPI, Request

from hosted_agent_kit.config.settings import Settings
from hosted_agent_kit.domain.errors import InsufficientScopeError, ValidationFailedError
from hosted_agent_kit.service.security.auth import Authenticator, JwksCache, TokenValidator
from hosted_agent_kit.service.security.claims import Principal

__all__ = ["EntraAuth"]


class EntraAuth:
    def __init__(
        self,
        *,
        tenant_id: str,
        audience: str | Sequence[str],
        authority_host: str = "https://login.microsoftonline.com",
        jwks_cache_seconds: int = 3600,
        leeway_seconds: int = 60,
        validator: TokenValidator | None = None,
    ) -> None:
        """``validator`` is for tests. Otherwise signing keys come from the tenant's JWKS URL."""
        audiences = [audience] if isinstance(audience, str) else list(audience)
        self._settings = Settings(
            auth_mode="entra",
            entra_tenant_id=tenant_id,
            entra_audience=audiences,
            entra_authority_host=authority_host,
            jwks_cache_seconds=jwks_cache_seconds,
            jwt_leeway_seconds=leeway_seconds,
        )
        self._jwks: JwksCache | None = None
        if validator is None:
            host = authority_host.rstrip("/")
            self._jwks = JwksCache(f"{host}/{tenant_id}/discovery/v2.0/keys", jwks_cache_seconds)
            validator = TokenValidator(self._settings, self._jwks)
        self._authenticator = Authenticator(self._settings, validator)

    async def principal(self, request: Request) -> Principal:
        """Validate the request's bearer token. Raises an ``AuthenticationRequiredError`` (401)."""
        return await self._authenticator.authenticate(request)

    async def user(self, request: Request) -> str:
        """Dependency: the signed-in user's id (the ``oid`` claim).

        An app-only token names a service, not a person, so it is refused here (422): a service
        that acts for its users must tell its own callers apart and use ``user_for_service``.
        """
        principal = await self.principal(request)
        if principal.app_only:
            raise ValidationFailedError(
                "This endpoint needs a signed-in user. An app-only token names a service."
            )
        return principal.user_id

    def user_for_service(
        self, subject: Callable[[Request], str | None], *, delegate_role: str
    ) -> Callable[..., Any]:
        """Dependency for services that call on behalf of users.

        Accepts an app-only token that carries ``delegate_role`` and returns
        ``"<service>:<subject>"``, where ``subject`` reads the end user from the request. Only
        services you have given the role can name a user, so one service cannot reach another's
        users. A delegated token returns the signed-in user as usual.
        """

        async def dependency(request: Request) -> str:
            principal = await self.principal(request)
            if not principal.app_only:
                return principal.user_id
            if not principal.has(delegate_role):
                raise InsufficientScopeError(
                    "The caller lacks the delegate role needed to act for an end user."
                )
            who = subject(request)
            if not who:
                raise ValidationFailedError("The end user is required for an app-only caller.")
            return f"{principal.user_id}:{who}"

        return dependency

    def require_role(self, role: str) -> Callable[[Request], Awaitable[Principal]]:
        """Dependency for administration routes: the caller's token must carry ``role``."""

        async def dependency(request: Request) -> Principal:
            principal = await self.principal(request)
            if not principal.has(role):
                raise InsufficientScopeError("The caller lacks the required role.")
            return principal

        return dependency

    async def close(self) -> None:
        if self._jwks is not None:
            await self._jwks.close()

    def install(self, app: FastAPI) -> None:
        """Close the signing-key client when the app shuts down."""
        inner = app.router.lifespan_context

        @asynccontextmanager
        async def lifespan(scope_app: Any) -> AsyncIterator[Any]:
            async with AsyncExitStack() as stack:
                stack.push_async_callback(self.close)
                state = await stack.enter_async_context(inner(scope_app))
                yield state

        app.router.lifespan_context = lifespan
