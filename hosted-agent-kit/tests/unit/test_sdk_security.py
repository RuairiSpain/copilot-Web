"""Safer defaults for developers: Entra sign-in, guarded routers and keyed identity hashes."""

from __future__ import annotations

from typing import Any

import httpx
import pytest
from fastapi import Depends, FastAPI
from fastapi.testclient import TestClient

from hosted_agent_kit import Hack
from hosted_agent_kit.integrations.entra import EntraAuth
from hosted_agent_kit.integrations.fastapi import KitDep, admin_router, install, reporting_router
from hosted_agent_kit.logging_config import configure_identifier_hashing, hash_identifier
from hosted_agent_kit.service.security.auth import JwksCache, TokenValidator
from hosted_agent_kit.testing import DemoFoundry
from tests.fakes.entra import AUDIENCE, OID, TENANT, JwksServer, token

DOC = {"agentPool": {"agents": {"chat": {"mode": "stateless"}}}}


def make_auth() -> EntraAuth:
    server = JwksServer()
    client = httpx.AsyncClient(transport=httpx.MockTransport(server.handler))
    probe = EntraAuth(tenant_id=TENANT, audience=AUDIENCE)
    jwks = JwksCache("https://keys.invalid", 3600, client=client)
    validator = TokenValidator(probe._settings, jwks)
    return EntraAuth(tenant_id=TENANT, audience=AUDIENCE, validator=validator)


def bearer(**overrides: Any) -> dict[str, str]:
    return {"Authorization": f"Bearer {token(**overrides)}"}


def build_app(auth: EntraAuth) -> FastAPI:
    app = FastAPI()
    install(app, Hack.from_dict(DOC, adapter=DemoFoundry()))
    auth.install(app)

    @app.post("/ask")
    async def ask(kit: KitDep, user: str = Depends(auth.user)) -> Any:
        return (await kit.ask("chat", "hi", user_id=user)).json() | {"user": user}

    @app.post("/service-ask")
    async def service_ask(
        kit: KitDep,
        user: str = Depends(
            auth.user_for_service(
                lambda r: r.headers.get("x-end-user"), delegate_role="Pool.Delegate"
            )
        ),
    ) -> Any:
        return {"user": user}

    app.include_router(
        admin_router(dependencies=[Depends(auth.require_role("Pool.Admin"))]), prefix="/ops"
    )
    return app


# ------------------------------------------------------------------- routers


def test_routers_refuse_to_build_without_a_guard() -> None:
    with pytest.raises(ValueError, match="dependencies="):
        reporting_router()
    with pytest.raises(ValueError, match="allow_unauthenticated"):
        admin_router()
    assert reporting_router(allow_unauthenticated=True)
    assert admin_router(dependencies=[Depends(lambda: None)])


# ------------------------------------------------------------------- Entra


def test_a_valid_token_gives_the_oid_and_a_missing_one_is_401() -> None:
    with TestClient(build_app(make_auth())) as client:
        ok = client.post("/ask", headers=bearer())
        assert ok.status_code == 200 and ok.json()["user"] == OID
        missing = client.post("/ask")
        assert missing.status_code == 401
        assert missing.headers["www-authenticate"].startswith("Bearer")
        assert client.post("/ask", headers=bearer(aud="api://other")).status_code == 401
        assert client.post("/ask", headers=bearer(tid="x")).status_code == 401
        assert client.post("/ask", headers=bearer(exp=1)).status_code == 401


def test_an_app_only_token_cannot_act_as_a_user() -> None:
    with TestClient(build_app(make_auth())) as client:
        refused = client.post("/ask", headers=bearer(scp=None))
        assert refused.status_code == 422


def test_a_service_needs_the_delegate_role_to_name_an_end_user() -> None:
    with TestClient(build_app(make_auth())) as client:
        headers = {**bearer(scp=None, roles=["Pool.Delegate"]), "x-end-user": "alice"}
        assert client.post("/service-ask", headers=headers).json() == {"user": f"{OID}:alice"}
        no_role = {**bearer(scp=None, roles=["Pool.Invoke"]), "x-end-user": "alice"}
        assert client.post("/service-ask", headers=no_role).status_code == 403
        no_user = bearer(scp=None, roles=["Pool.Delegate"])
        assert client.post("/service-ask", headers=no_user).status_code == 422
        delegated = client.post("/service-ask", headers=bearer())  # a signed-in user: as usual
        assert delegated.json() == {"user": OID}


def test_the_admin_dependency_checks_the_role() -> None:
    path = "/ops/agents/chat/sync"
    with TestClient(build_app(make_auth())) as client:
        assert client.post(path).status_code == 401
        denied = client.post(path, headers=bearer(roles=["Pool.Invoke"]))
        assert denied.status_code == 403
        allowed = client.post(path, headers=bearer(roles=["Pool.Admin"]))
        assert allowed.status_code == 200


# ------------------------------------------------------------------- hashing


def test_identifier_tokens_depend_on_a_secret() -> None:
    configure_identifier_hashing(b"a" * 32)
    first = hash_identifier("alice@example.com")
    configure_identifier_hashing(b"a" * 32)
    assert hash_identifier("alice@example.com") == first  # stable for the same secret
    configure_identifier_hashing(b"b" * 32)
    assert hash_identifier("alice@example.com") != first  # another secret, another token
    import hashlib

    unkeyed = hashlib.sha256(b"alice@example.com").hexdigest()[:16]
    assert first != unkeyed  # not reproducible without the secret
