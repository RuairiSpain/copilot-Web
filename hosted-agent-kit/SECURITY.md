# Security policy

## Reporting a vulnerability

Do not open a public issue for a security problem. Use the private vulnerability reporting feature
of the repository host (for GitHub, "Report a vulnerability" under the Security tab). Include the
version, the affected component, steps to reproduce and the impact you expect. Expect an
acknowledgement within three working days.

## Supported versions

Only the latest 1.x release receives security fixes.

## Security model

- **Callers** authenticate with Microsoft Entra ID bearer tokens. The service validates the
  signature (RS256 only), issuer, audience, expiry and tenant. Signing keys are cached and refreshed
  at a rate-limited pace, so a flood of unknown key ids cannot force repeated key downloads.
- **User identity** is the `oid` claim. A user id in a request body is rejected unless
  `POOL_AUTH_MODE=development`.
- **Roles** `Pool.Invoke`, `Pool.Admin` and `Pool.Diagnostics` separate calling agents,
  administering the pool and seeing raw user identifiers. The role names are configurable.
- **Foundry access** uses a managed identity or another `DefaultAzureCredential` source. No keys or
  connection strings are stored in configuration.
- **Session identifiers** never appear in client responses, metrics or logs. Logs carry a one-way
  hash. Admin endpoints return them to administrators. `X-Pool-Session-Id` is off by default and
  limited to the diagnostics role.
- **Session id key.** `POOL_SESSION_ID_KEY` derives each stateful user's session id. It is held as a
  secret and never logged or returned. Anyone with the key can compute a user's session id, but
  they still need a valid caller identity to reach the session through this service. Store it in a
  secret store, keep it stable and treat a change as an event that orphans sessions. A user can only
  derive their own session id, so one user cannot take another's session.
- **Isolation key.** `POOL_FOUNDRY_ISOLATION_KEY` is a partition value, not a credential. Foundry
  states that it narrows which sessions a caller acts on and is not an authentication mechanism.
- **Content**: prompts, responses, tokens and authorisation headers are never logged. Log fields
  with sensitive names are redacted.
- **Input**: request bodies are size-limited, models reject unknown fields, path parameters are
  pattern-checked and validation errors never echo submitted values.
- **Configuration** is parsed with a safe YAML loader that rejects duplicate keys and unknown keys.
- **Container** runs as a non-root user with no build tools, a read-only root file system in Compose
  and no added capabilities.

## Operational guidance

- Never run `POOL_AUTH_MODE=development` outside a developer machine. It disables authentication.
  The service logs a warning at start-up when it is set.
- Generate `POOL_SESSION_ID_KEY` from a cryptographically secure source (at least 32 characters).
- Keep the service on one replica. A second replica could lease the same session twice.
- Grant the service identity the smallest Foundry role that allows session management.
- Treat `/v1/admin/*` and `/metrics` as internal. Restrict them at the network edge if possible.

## Verification in this repository

CI runs Ruff, strict mypy, Bandit, `pip-audit`, the test suite, an image SBOM and a Trivy scan.
