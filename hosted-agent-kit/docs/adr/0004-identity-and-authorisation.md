# ADR 0004: Caller identity and roles

Status: accepted

## Context

Stateful routing keys sessions on the caller's identity, so the identity must be stable and must not
be spoofable.

## Decision

- Callers present Microsoft Entra ID bearer tokens. The service accepts RS256 only and checks
  signature, issuer (v1 and v2 forms for the configured tenant), audience, expiry and `tid`.
- `user_id` is the **`oid` claim**: the immutable object id of the user or service principal. It is
  never read from a request body outside `POOL_AUTH_MODE=development`.
- Roles come from the `roles` and `scp` claims: `Pool.Invoke`, `Pool.Admin`, `Pool.Diagnostics`.
- Signing keys are cached for an hour. An unknown key id triggers a refresh at most every 30 seconds.
- Admin responses redact user ids as `u_<hash>` unless the caller has the diagnostics role.

## Consequences

- An app-only token has the service principal's `oid`, so service callers get their own sessions.
- A user id mapped to the same person through a different tenant identity is a different user.
- App roles must be created in Entra by hand. Bicep cannot create them.
