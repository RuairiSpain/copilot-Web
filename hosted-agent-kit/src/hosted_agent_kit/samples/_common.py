"""Shared plumbing for the samples. Your own application does not need this module.

``load_kit`` returns a ``Hack`` for one of the YAML files in this directory. With no
``FOUNDRY_PROJECT_ENDPOINT`` set it runs in demo mode: an in-memory agent answers, so every
sample works offline. Set ``FOUNDRY_PROJECT_ENDPOINT`` (and sign in with ``az login``) to use
real hosted agents.
"""

from __future__ import annotations

import hmac
import os
from pathlib import Path
from typing import Annotated

from fastapi import Depends, Header

from hosted_agent_kit import Hack
from hosted_agent_kit.errors import HackError
from hosted_agent_kit.testing import DemoFoundry

SAMPLES_DIR = Path(os.environ.get("HACK_SAMPLES_DIR", Path(__file__).parent))


def demo_mode() -> bool:
    return not os.environ.get("FOUNDRY_PROJECT_ENDPOINT")


def load_kit(relative_path: str) -> Hack:
    """Create the kit for ``samples/<relative_path>``."""
    path = SAMPLES_DIR / relative_path
    return Hack.from_yaml(path, adapter=DemoFoundry() if demo_mode() else None)


class Unauthenticated(HackError):
    status = 401
    code = "SAMPLE_USER_REQUIRED"
    title = "Send an X-User-Id header"
    phase = "auth"
    retry_safe = True


def current_user(x_user_id: Annotated[str | None, Header()] = None) -> str:
    """The signed-in user. A real application reads this from its own authentication.

    The samples trust an ``X-User-Id`` header so they can be tried with curl. Never do that in
    production: take the id from a validated token (for example the Entra ``oid`` claim).
    """
    if not x_user_id:
        raise Unauthenticated()
    return x_user_id


UserDep = Annotated[str, Depends(current_user)]


class AdminKeyRequired(HackError):
    status = 403
    code = "SAMPLE_ADMIN_KEY_REQUIRED"
    title = "Send the admin key in X-Admin-Key"
    phase = "auth"
    retry_safe = True


def require_admin_key(x_admin_key: Annotated[str | None, Header()] = None) -> None:
    """Guard for administrative endpoints.

    The samples compare a header with ``HACK_SAMPLE_ADMIN_KEY`` (default ``dev-admin-key``).
    Replace it with your real authorisation, for example a role check on a validated token.
    """
    expected = os.environ.get("HACK_SAMPLE_ADMIN_KEY", "dev-admin-key")
    if not x_admin_key or not hmac.compare_digest(x_admin_key, expected):
        raise AdminKeyRequired()


AdminDep = Depends(require_admin_key)
