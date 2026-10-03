"""Principal extraction from validated token claims.

``user_id`` is the Entra ``oid`` claim: the immutable object id of the user (or service
principal) within the tenant. The service validates a single configured tenant, so ``oid``
is unique within the trust boundary and is never taken from request bodies in production.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from hosted_agent_kit.domain.errors import AuthenticationRequiredError
from hosted_agent_kit.domain.identity import is_valid_user_id

USER_ID_CLAIM = "oid"


@dataclass(frozen=True)
class Principal:
    user_id: str
    roles: frozenset[str]
    # True for a service principal acting as itself. Delegated (user) tokens carry ``scp``.
    app_only: bool = False

    def has(self, role: str) -> bool:
        return role in self.roles


def _roles(claims: dict[str, Any]) -> frozenset[str]:
    roles: set[str] = set()
    app_roles = claims.get("roles")
    if isinstance(app_roles, list):
        roles.update(str(r) for r in app_roles)
    scopes = claims.get("scp")
    if isinstance(scopes, str):
        roles.update(scopes.split())
    return frozenset(roles)


def principal_from_claims(claims: dict[str, Any]) -> Principal:
    user_id = claims.get(USER_ID_CLAIM)
    if not isinstance(user_id, str) or not is_valid_user_id(user_id):
        raise AuthenticationRequiredError("The token does not carry a usable user identity.")
    app_only = claims.get("idtyp") == "app" or "scp" not in claims
    return Principal(user_id=user_id, roles=_roles(claims), app_only=app_only)


__all__ = ["USER_ID_CLAIM", "Principal", "is_valid_user_id", "principal_from_claims"]
