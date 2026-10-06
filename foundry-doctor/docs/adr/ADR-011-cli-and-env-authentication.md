# ADR-011: Authentication sources stay CLI and environment based

Status: Accepted  
Date: 2026-10-06

## Context

Foundry Doctor needs Azure access for preflight, runtime, graph, and some
cost/account metadata operations. The product is intentionally read-only and
tries to keep the dependency footprint small.

V1 also needs clear guidance for GitHub Actions OIDC and managed identity
environments without introducing a separate credential SDK dependency only for
token acquisition.

## Decision

1. V1 authentication stays **CLI and environment based**:
   1. `AZURE_ACCESS_TOKEN`
   2. `azd auth token`
   3. `az account get-access-token`
2. Foundry Doctor does **not** add `azidentity`-style login orchestration for
   acquiring tokens directly. The tool consumes credentials the operator or CI
   environment has already established.
3. Managed identity guidance is documentation-only for V1:
   operators should obtain a bearer token outside Foundry Doctor and pass it as
   `AZURE_ACCESS_TOKEN`.
4. GitHub Actions guidance is documentation-only for V1:
   workflows should use OIDC login first, then invoke Foundry Doctor in the
   same job so the Azure CLI or azd token cache is already available.

## Consequences

- supply-chain and binary footprint stay smaller than adding another login
  stack purely for token acquisition
- credential behaviour remains deterministic and easy to explain
- docs must show how to use OIDC and managed identity with the existing token
  chain
