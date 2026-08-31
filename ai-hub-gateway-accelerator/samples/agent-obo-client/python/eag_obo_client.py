"""
EAG OBO Client — for agent/sub-agent developers inside the trust boundary.

Copy this file into your agent's codebase, fill in your Entra ID app
registration values, and use it before your agent calls out to a
sub-agent or MCP tool through the EAG gateway — so that call gets
metered against the ORIGINAL user's token budget, not your own service
identity, and is cryptographically verifiable rather than a
self-asserted claim.

Implements the design in guides/agent-hierarchy-attribution.md:
  - Section 4: Entra ID On-Behalf-Of (OBO) token exchange.
  - Section 1: the x-agent-root-id / x-agent-caller-type / x-agent-depth
    hierarchy headers, carried alongside the OBO token.

Requires: pip install msal requests
Verified against msal 1.38.0's actual method signatures — not guessed
from documentation alone.
"""

import base64
import json
import uuid
from typing import Optional

import msal


class EagOboError(Exception):
    """Raised when the OBO token exchange fails.

    Check `.error_detail` for the raw MSAL error response before
    assuming this is a bug in this file — the overwhelmingly common
    cause is an Entra ID app-registration/consent problem, not a code
    problem:
      - Your app's registration hasn't been granted (and admin-
        consented) the delegated permission to call the gateway on the
        user's behalf.
      - The incoming token you passed as `user_assertion` is an ID
        token, not an access token — OBO requires an access token.
      - The incoming token's audience doesn't match what your app
        registration expects.
    """

    def __init__(self, message: str, error_detail: dict):
        super().__init__(message)
        self.error_detail = error_detail


class EagOboClient:
    """
    Wraps an Entra ID confidential client configured for On-Behalf-Of
    token exchange. Create one instance per process/app and reuse it
    across requests — MSAL caches tokens internally and skips the
    network round trip when it already holds a still-valid token.
    """

    def __init__(self, client_id: str, client_credential, tenant_id: str):
        """
        `client_credential` can be a plain client secret string (the
        simplest way to get started) or a certificate/assertion dict
        for MSAL's client-assertion support. Prefer a certificate or
        Federated Identity Credential over a bare secret in production
        — this project's own pricing-service uses managed identity for
        exactly this reason (see src/pricing-service/), and Microsoft's
        purpose-built "Microsoft Entra Agent ID" product (referenced in
        guides/agent-hierarchy-attribution.md, section 4) is built
        around Federated Identity Credentials rather than a stored
        secret. See:
        https://learn.microsoft.com/en-us/entra/msal/python/advanced/client-assertions
        """
        self._app = msal.ConfidentialClientApplication(
            client_id=client_id,
            client_credential=client_credential,
            authority=f"https://login.microsoftonline.com/{tenant_id}",
        )

    def get_gateway_token(self, user_assertion: str, gateway_scopes: list) -> str:
        """
        Exchanges the incoming user's ACCESS token for one scoped to
        call the EAG gateway, still carrying the original user's
        identity (via the `oid` claim — see extract_oid() below).

        `user_assertion` — the access token your agent received on its
        own inbound request. Must be an access token, not an ID token.

        `gateway_scopes` — the scope(s) exposed by the gateway's own
        Entra ID app registration, e.g.
        ["api://<gateway-app-id>/.default"]. This is specific to your
        tenant's setup — ask whoever owns the gateway's app
        registration for the exact scope string rather than guessing.

        Raises EagOboError on failure.
        """
        result = self._app.acquire_token_on_behalf_of(
            user_assertion=user_assertion,
            scopes=gateway_scopes,
        )
        if "access_token" not in result:
            raise EagOboError(
                "OBO token exchange failed: {} — {}".format(
                    result.get("error"), result.get("error_description")
                ),
                error_detail=result,
            )
        return result["access_token"]


def extract_oid(jwt_token: str) -> Optional[str]:
    """
    Pulls the `oid` claim out of a JWT WITHOUT verifying its signature.

    This is intentional and fine for its actual purpose here — labeling
    YOUR OWN outbound request/log lines — but it is NOT a substitute for
    real validation, and its output must never be used to make an
    authorization decision in your own code. The EAG gateway
    independently validates every token's signature, issuer, and
    audience on every call (guides/agent-hierarchy-attribution.md,
    section 4); this function exists only so your logging/telemetry can
    show the right value, not so your code can decide whether to trust
    the caller.

    Deliberately reads `oid`, not `sub` — `sub` is pairwise-pseudonymous
    per resource in Entra ID and will NOT match the same user across
    different audiences/hops. Using it here would silently break the
    chargeback roll-up this whole mechanism exists for.
    """
    try:
        payload_b64 = jwt_token.split(".")[1]
        padding = "=" * (-len(payload_b64) % 4)
        payload = json.loads(base64.urlsafe_b64decode(payload_b64 + padding))
        return payload.get("oid")
    except (IndexError, ValueError, TypeError):
        return None


def build_hierarchy_headers(
    gateway_access_token: str,
    root_id: Optional[str] = None,
    caller_type: str = "sub-agent",
    depth: int = 1,
) -> dict:
    """
    Builds the Authorization + x-agent-* headers for a call into the
    EAG gateway (design doc section 1) — send these alongside the OBO
    token from get_gateway_token().

    `root_id` — pass through the value YOUR OWN caller gave you, if you
    received one (you're a sub-agent being called by another agent).
    Leave it None if you're the first hop — a fresh ID is generated for
    you. Whatever value ends up here, everything YOU call downstream
    must propagate the identical value onward, unchanged — that's what
    makes it a roll-up key.

    `caller_type` — "root" for the first hop, "sub-agent" or "mcp-tool"
    otherwise, matching whichever your app actually is.

    `depth` — your own incoming depth plus one. Defaults to 1
    ("one hop below the root"); pass 0 explicitly if this call IS the
    root.
    """
    return {
        "Authorization": "Bearer {}".format(gateway_access_token),
        "x-agent-root-id": root_id or str(uuid.uuid4()),
        "x-agent-caller-type": caller_type,
        "x-agent-depth": str(depth),
    }


# ---------------------------------------------------------------------
# Example usage — a sub-agent receiving a request and calling the
# gateway on behalf of whoever called it. Adapt the web-framework parts
# (this shows plain WSGI-style header access) to your own framework.
# ---------------------------------------------------------------------
if __name__ == "__main__":
    import os

    import requests

    obo_client = EagOboClient(
        client_id=os.environ["AGENT_CLIENT_ID"],
        client_credential=os.environ["AGENT_CLIENT_SECRET"],  # swap for a certificate in production
        tenant_id=os.environ["ENTRA_TENANT_ID"],
    )

    def handle_incoming_request(inbound_headers: dict, inbound_body: dict) -> dict:
        # 1. Pull the caller's token and hierarchy context off the
        #    inbound request this sub-agent just received.
        inbound_authorization = inbound_headers.get("Authorization", "")
        incoming_user_token = inbound_authorization.removeprefix("Bearer ").strip()
        incoming_root_id = inbound_headers.get("x-agent-root-id")
        incoming_depth = int(inbound_headers.get("x-agent-depth", "0"))

        # 2. Exchange it for a token scoped to the gateway.
        gateway_token = obo_client.get_gateway_token(
            user_assertion=incoming_user_token,
            gateway_scopes=[os.environ["GATEWAY_SCOPE"]],  # e.g. "api://<gateway-app-id>/.default"
        )

        # 3. (Optional, for your own logging) confirm which user this
        #    resolved to — never use this to authorize anything.
        oid = extract_oid(incoming_user_token)
        print(f"Calling gateway on behalf of oid={oid}")

        # 4. Build the outbound headers and call the gateway.
        headers = build_hierarchy_headers(
            gateway_access_token=gateway_token,
            root_id=incoming_root_id,       # propagate unchanged
            caller_type="sub-agent",
            depth=incoming_depth + 1,
        )
        response = requests.post(
            os.environ["GATEWAY_URL"],
            headers=headers,
            json=inbound_body,
            timeout=60,
        )
        response.raise_for_status()
        return response.json()
