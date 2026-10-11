import asyncio
import json
import time
from dataclasses import replace

import httpx
import jwt
import pytest
from cryptography.hazmat.primitives.asymmetric import rsa
from fastapi.testclient import TestClient

from decision_router.app import create_app
from decision_router.inbound import InboundAuth, Unauthorized
from decision_router.testing import ranked

TENANT = "11111111-2222-3333-4444-555555555555"
AUDIENCE = "api://decision-router"
AGENT_IDENTITY = "aaaaaaaa-0000-0000-0000-000000000001"
USER = [{"role": "user", "content": "hi"}]

PRIVATE_KEY = rsa.generate_private_key(public_exponent=65537, key_size=2048)
JWK = {**json.loads(jwt.algorithms.RSAAlgorithm.to_jwk(PRIVATE_KEY.public_key())), "kid": "k1", "use": "sig"}


def token(**claims) -> str:
    now = int(time.time())
    body = {"iss": f"https://login.microsoftonline.com/{TENANT}/v2.0", "aud": AUDIENCE, "tid": TENANT,
            "azp": AGENT_IDENTITY, "iat": now, "nbf": now, "exp": now + 600, **claims}
    return jwt.encode(body, PRIVATE_KEY, algorithm="RS256", headers={"kid": claims.pop("kid", "k1")})


def keys_http(counter: list | None = None) -> httpx.AsyncClient:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == f"/{TENANT}/discovery/v2.0/keys"
        if counter is not None:
            counter.append(1)
        return httpx.Response(200, json={"keys": [JWK]})
    return httpx.AsyncClient(transport=httpx.MockTransport(handler))


def entra(**kwargs) -> InboundAuth:
    return InboundAuth(tenant_id=TENANT, audiences=(AUDIENCE,), http=keys_http(), **kwargs)


def check(gate: InboundAuth, headers: dict):
    return asyncio.run(gate.check(httpx.Headers(headers)))


@pytest.mark.parametrize("headers", [{"api-key": "k2"}, {"x-api-key": "k2"}, {"Authorization": "Bearer k2"}])
def test_api_key_in_any_supported_header(headers):
    caller = check(InboundAuth(api_keys=("k1", "k2")), headers)
    assert (caller.method, caller.identity) == ("api_key", "2")


@pytest.mark.parametrize("headers, message", [({}, "missing credentials"), ({"api-key": "nope"}, "invalid")])
def test_bad_or_missing_key(headers, message):
    with pytest.raises(Unauthorized, match=message):
        check(InboundAuth(api_keys=("k1",)), headers)


def test_entra_token_from_a_managed_identity_or_client_credentials():
    caller = check(entra(), {"Authorization": f"Bearer {token()}"})
    assert (caller.method, caller.identity) == ("entra", AGENT_IDENTITY)
    v1 = token(iss=f"https://sts.windows.net/{TENANT}/", azp=None, appid=AGENT_IDENTITY)
    assert check(entra(), {"Authorization": f"Bearer {v1}"}).identity == AGENT_IDENTITY


@pytest.mark.parametrize("claims, message", [
    ({"aud": "api://something-else"}, "token rejected"),
    ({"exp": int(time.time()) - 3600}, "token rejected"),
    ({"iss": "https://login.microsoftonline.com/other-tenant/v2.0"}, "token rejected"),
    ({"kid": "unknown"}, "unknown key"),
])
def test_entra_token_checks(claims, message):
    with pytest.raises(Unauthorized, match=message):
        check(entra(), {"Authorization": f"Bearer {token(**claims)}"})


def test_allowed_client_ids():
    gate = entra(allowed_client_ids=("bbbbbbbb-0000-0000-0000-000000000002",))
    with pytest.raises(Unauthorized, match="ROUTER_ENTRA_ALLOWED_CLIENT_IDS"):
        check(gate, {"Authorization": f"Bearer {token()}"})


def test_either_credential_works_when_both_are_configured():
    gate = InboundAuth(api_keys=("k1",), tenant_id=TENANT, audiences=(AUDIENCE,), http=keys_http())
    assert check(gate, {"api-key": "k1"}).method == "api_key"
    assert check(gate, {"Authorization": f"Bearer {token()}"}).method == "entra"


def test_signing_keys_are_cached():
    calls = []
    gate = InboundAuth(tenant_id=TENANT, audiences=(AUDIENCE,), http=keys_http(calls))
    for _ in range(3):
        check(gate, {"Authorization": f"Bearer {token()}"})
    with pytest.raises(Unauthorized):
        check(gate, {"Authorization": f"Bearer {token(kid='rotated')}"})  # refetch is rate-limited
    assert len(calls) == 1


def test_refuses_to_start_without_inbound_auth():
    with pytest.raises(ValueError, match="no inbound authentication"):
        InboundAuth()
    with pytest.raises(ValueError, match="both"):
        InboundAuth(tenant_id=TENANT)
    assert InboundAuth(allow_unauthenticated=True).methods == []


def test_app_rejects_unauthenticated_calls_but_not_health(make_pipeline, settings):
    secured = replace(settings, allow_unauthenticated=False, api_keys=("secret",))
    with TestClient(create_app(secured, pipeline=make_pipeline())) as client:
        denied = client.post("/v1/chat/completions", json={"messages": USER})
        assert denied.status_code == 401 and denied.headers["www-authenticate"] == "Bearer"
        assert denied.json()["error"]["code"] == "unauthorized"
        assert client.get("/health/ready").json()["inbound_auth"] == ["api_key"]
        assert client.get("/health/live").status_code == 200


def test_profile_from_model_name(make_pipeline, settings, fake, tmp_path):
    fake.decision_responses.append(ranked(["claude-opus-5"]))
    secured = replace(settings, allow_unauthenticated=False, api_keys=("secret",))
    with TestClient(create_app(secured, pipeline=make_pipeline())) as client:
        response = client.post("/v1/chat/completions", headers={"api-key": "secret"},
                               json={"model": "decision-router-quality", "messages": USER})
    assert response.status_code == 200 and response.headers["x-router-mode"] == "quality"
    event = json.loads((tmp_path / "decisions.jsonl").read_text().splitlines()[-1])
    assert event["profile"] == "decision-router-quality"
    assert event["caller"] == {"method": "api_key", "identity": "1"}


def test_request_fields_override_the_profile_and_unknown_names_are_ignored(make_pipeline, settings):
    profiles = {**settings.profiles, "agent-eu": {"routing_mode": "cost", "compatibility": "new",
                                                  "routing_constraints": {"inference_in_azure": True}}}
    pipe = make_pipeline(profiles=profiles)
    eu = pipe.prepare({"model": "agent-eu", "messages": USER})
    assert (eu.mode, eu.catalog.compatibility, eu.constraints.inference_in_azure) == ("cost", "new", True)
    assert pipe.prepare({"model": "agent-eu", "routing_mode": "quality", "messages": USER}).mode == "quality"
    plain = pipe.prepare({"model": "model-router", "messages": USER})
    assert plain.profile is None and plain.mode == "balanced" and "model" not in plain.body
