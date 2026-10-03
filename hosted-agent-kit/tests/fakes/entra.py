"""Entra token helpers shared by unit and API tests."""

from __future__ import annotations

import time
from typing import Any

import httpx
import jwt
from cryptography.hazmat.primitives.asymmetric import rsa
from jwt.algorithms import RSAAlgorithm

TENANT = "11111111-2222-3333-4444-555555555555"
AUDIENCE = "api://pool"
OID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"


def new_key() -> rsa.RSAPrivateKey:
    return rsa.generate_private_key(public_exponent=65537, key_size=2048)


KEY = new_key()
OTHER_KEY = new_key()


def jwk(key: rsa.RSAPrivateKey, kid: str) -> dict[str, Any]:
    data = RSAAlgorithm.to_jwk(key.public_key(), as_dict=True)
    return {**data, "kid": kid, "use": "sig"}


def token(
    key: rsa.RSAPrivateKey = KEY,
    kid: str = "k1",
    headers: dict[str, Any] | None = None,
    **overrides: Any,
) -> str:
    now = int(time.time())
    claims: dict[str, Any] = {
        "iss": f"https://login.microsoftonline.com/{TENANT}/v2.0",
        "aud": AUDIENCE,
        "tid": TENANT,
        "oid": OID,
        "roles": ["Pool.Invoke"],
        "scp": "access_as_user",  # a delegated token. Pass scp=None for an app-only token
        "exp": now + 600,
        "nbf": now - 5,
    }
    claims.update(overrides)
    claims = {k: v for k, v in claims.items() if v is not None}
    return jwt.encode(claims, key, algorithm="RS256", headers={"kid": kid, **(headers or {})})


class JwksServer:
    def __init__(self) -> None:
        self.keys = [jwk(KEY, "k1")]
        self.fetches = 0
        self.status = 200

    def handler(self, request: httpx.Request) -> httpx.Response:
        self.fetches += 1
        if self.status != 200:
            return httpx.Response(self.status)
        extra = [{"kty": "EC", "kid": "ignored"}, {"kty": "RSA"}]  # unusable entries are skipped
        return httpx.Response(200, json={"keys": [*self.keys, *extra]})


class Clock:
    now = 1000.0

    def __call__(self) -> float:
        return self.now
