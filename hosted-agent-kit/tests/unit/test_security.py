"""Entra token validation, claim handling and role enforcement."""

from __future__ import annotations

import time
from typing import Any

import httpx
import jwt
import pytest
from starlette.requests import Request

from hosted_agent_kit.domain.errors import AuthenticationRequiredError, InsufficientScopeError
from hosted_agent_kit.service.security.auth import Authenticator, JwksCache, TokenValidator
from hosted_agent_kit.service.security.claims import (
    Principal,
    is_valid_user_id,
    principal_from_claims,
)
from tests.conftest import make_settings
from tests.fakes.entra import (
    AUDIENCE,
    KEY,
    OID,
    OTHER_KEY,
    TENANT,
    Clock,
    JwksServer,
    jwk,
    token,
)


def make_validator(
    server: JwksServer, clock: Clock | None = None, **settings: Any
) -> TokenValidator:
    s = make_settings(
        auth_mode="entra", entra_tenant_id=TENANT, entra_audience=[AUDIENCE], **settings
    )
    client = httpx.AsyncClient(transport=httpx.MockTransport(server.handler))
    jwks = JwksCache("https://keys.test/keys", s.jwks_cache_seconds, client, clock or Clock())
    return TokenValidator(s, jwks)


async def test_valid_token_yields_claims() -> None:
    claims = await make_validator(JwksServer()).validate(token())
    assert claims["oid"] == OID and claims["roles"] == ["Pool.Invoke"]


async def test_v1_issuer_is_accepted() -> None:
    claims = await make_validator(JwksServer()).validate(
        token(iss=f"https://sts.windows.net/{TENANT}/")
    )
    assert claims["tid"] == TENANT


@pytest.mark.parametrize(
    "bad",
    [
        lambda: token(exp=int(time.time()) - 3600),
        lambda: token(aud="api://someone-else"),
        lambda: token(iss="https://evil.example/v2.0"),
        lambda: token(tid="99999999-0000-0000-0000-000000000000"),
        lambda: token(tid=None),
        lambda: token(exp=None),
        lambda: token(nbf=int(time.time()) + 3600),
        lambda: token(key=OTHER_KEY),  # signed with a key the tenant does not publish
        lambda: token(kid="unknown"),
        lambda: "not-a-jwt",
        lambda: "",
    ],
)
async def test_invalid_tokens_are_rejected_with_invalid_token_challenge(bad: Any) -> None:
    with pytest.raises(AuthenticationRequiredError) as info:
        await make_validator(JwksServer()).validate(bad())
    assert 'error="invalid_token"' in info.value.headers["WWW-Authenticate"]


async def test_algorithm_confusion_and_none_are_rejected() -> None:
    server = JwksServer()
    public_pem_as_secret = "x" * 64
    hs = jwt.encode(
        {"iss": "x", "aud": AUDIENCE, "exp": int(time.time()) + 60},
        public_pem_as_secret,
        algorithm="HS256",
        headers={"kid": "k1"},
    )
    with pytest.raises(AuthenticationRequiredError):
        await make_validator(server).validate(hs)
    unsigned = jwt.encode(
        {"aud": AUDIENCE, "exp": int(time.time()) + 60},
        "",
        algorithm="none",
        headers={"kid": "k1"},
    )
    with pytest.raises(AuthenticationRequiredError):
        await make_validator(server).validate(unsigned)
    no_kid = jwt.encode({"aud": AUDIENCE}, KEY, algorithm="RS256")
    with pytest.raises(AuthenticationRequiredError):
        await make_validator(server).validate(no_kid)


async def test_jwks_failure_is_an_authentication_failure_not_a_crash() -> None:
    server = JwksServer()
    server.status = 500
    with pytest.raises(AuthenticationRequiredError):
        await make_validator(server).validate(token())


async def test_keys_are_cached_until_ttl_then_refreshed() -> None:
    server, clock = JwksServer(), Clock()
    validator = make_validator(server, clock, jwks_cache_seconds=100)
    await validator.validate(token())
    await validator.validate(token())
    assert server.fetches == 1
    clock.now += 101
    await validator.validate(token())
    assert server.fetches == 2


async def test_key_rotation_triggers_a_rate_limited_refresh() -> None:
    server, clock = JwksServer(), Clock()
    validator = make_validator(server, clock)
    await validator.validate(token())
    server.keys = [jwk(KEY, "k1"), jwk(OTHER_KEY, "k2")]
    rotated = token(key=OTHER_KEY, kid="k2")
    with pytest.raises(AuthenticationRequiredError):  # inside the 30 second guard
        await validator.validate(rotated)
    assert server.fetches == 1
    clock.now += 31
    await validator.validate(rotated)
    assert server.fetches == 2


async def test_unknown_kid_flood_cannot_force_repeated_fetches() -> None:
    server, clock = JwksServer(), Clock()
    validator = make_validator(server, clock)
    for _ in range(5):
        with pytest.raises(AuthenticationRequiredError):
            await validator.validate(token(kid="nope"))
    assert server.fetches == 1


async def test_jwks_close_only_closes_clients_it_owns() -> None:
    owned = JwksCache("https://k", 10)
    await owned.close()
    assert owned._client.is_closed
    shared = httpx.AsyncClient()
    cache = JwksCache("https://k", 10, shared)
    await cache.close()
    assert not shared.is_closed
    await shared.aclose()


# -------------------------------------------------------------------- claims


def test_user_id_comes_from_oid_and_roles_merge_app_roles_and_scopes() -> None:
    principal = principal_from_claims(
        {"oid": OID, "roles": ["A", "B"], "scp": "C D", "sub": "ignored"}
    )
    assert principal.user_id == OID and principal.roles == {"A", "B", "C", "D"}
    assert principal.has("C") and not principal.has("Z")


def test_roles_tolerate_missing_or_malformed_claims() -> None:
    assert principal_from_claims({"oid": OID, "roles": "nope", "scp": ["x"]}).roles == frozenset()


@pytest.mark.parametrize(
    "claims",
    [
        {},
        {"oid": ""},
        {"oid": 5},
        {"oid": "a/b"},
        {"oid": "../x"},
        {"oid": "x" * 200},
        {"sub": "only-sub"},
    ],
)
def test_missing_or_unsafe_oid_is_rejected(claims: dict[str, Any]) -> None:
    with pytest.raises(AuthenticationRequiredError):
        principal_from_claims(claims)


def test_user_id_validator() -> None:
    assert is_valid_user_id(OID) and not is_valid_user_id("a b") and not is_valid_user_id("")


# ------------------------------------------------------------- authenticator


def request_with(headers: dict[str, str]) -> Request:
    scope = {
        "type": "http",
        "method": "POST",
        "path": "/",
        "headers": [(k.lower().encode(), v.encode()) for k, v in headers.items()],
    }
    return Request(scope)


def entra_auth(server: JwksServer | None = None) -> Authenticator:
    settings = make_settings(auth_mode="entra", entra_tenant_id=TENANT, entra_audience=[AUDIENCE])
    return Authenticator(settings, make_validator(server or JwksServer()))


@pytest.mark.parametrize("header", ["", "Basic abc", "Bearer", "Bearer   ", "bearer"])
async def test_missing_or_malformed_authorization_gets_a_challenge(header: str) -> None:
    headers = {"Authorization": header} if header else {}
    with pytest.raises(AuthenticationRequiredError) as info:
        await entra_auth().authenticate(request_with(headers))
    assert info.value.headers["WWW-Authenticate"].startswith("Bearer")


async def test_valid_bearer_produces_principal() -> None:
    principal = await entra_auth().authenticate(
        request_with({"Authorization": f"bearer {token()}"})
    )
    assert principal == Principal(
        user_id=OID, roles=frozenset({"Pool.Invoke", "access_as_user"}), app_only=False
    )


async def test_token_without_usable_oid_is_rejected_as_invalid() -> None:
    with pytest.raises(AuthenticationRequiredError) as info:
        await entra_auth().authenticate(
            request_with({"Authorization": f"Bearer {token(oid='bad id')}"})
        )
    assert 'error="invalid_token"' in info.value.headers["WWW-Authenticate"]


async def test_authorise_enforces_roles() -> None:
    auth = entra_auth()
    bearer = {"Authorization": f"Bearer {token()}"}
    assert (await auth.authorise(request_with(bearer), "Pool.Invoke")).user_id == OID
    with pytest.raises(InsufficientScopeError):
        await auth.authorise(request_with(bearer), "Pool.Admin")


async def test_scope_claim_satisfies_role_check() -> None:
    bearer = {"Authorization": f"Bearer {token(roles=None, scp='Pool.Invoke Other')}"}
    assert (await entra_auth().authorise(request_with(bearer), "Pool.Invoke")).has("Other")


async def test_development_mode_grants_all_roles_and_validates_the_dev_user() -> None:
    settings = make_settings(auth_mode="development")
    auth = Authenticator(settings, None)
    default = await auth.authenticate(request_with({}))
    assert default.user_id == "dev-user"
    assert default.roles == {"Pool.Invoke", "Pool.Admin", "Pool.Diagnostics"}
    assert (await auth.authenticate(request_with({"X-Dev-User-Id": "alice"}))).user_id == "alice"
    with pytest.raises(AuthenticationRequiredError):
        await auth.authenticate(request_with({"X-Dev-User-Id": "../etc"}))
