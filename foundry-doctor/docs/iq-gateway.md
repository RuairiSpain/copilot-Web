# Foundry IQ and AI gateway rules

Phase 8 adds:

- `FND-IQ-001` .. `FND-IQ-012` for Azure AI Search / Foundry IQ metadata
- `FND-GW-001` .. `FND-GW-006` for API Management AI gateway policy and routing checks

## Safety contract

- Metadata only. The implementation never reads document bodies, prompts,
  completions, or connection secrets.
- Secrets are classified, not echoed. Findings name the object and the unsafe
  pattern only.
- Missing metadata, unresolved expressions, external policy links, and absent
  live permissions degrade to skipped or uncertain results, never pass.

## Inputs

The Phase 8 rules correlate:

- compiled ARM / Bicep resources
- `azure.yaml` MCP connection metadata
- metadata-only live IQ/Search snapshot reads when the doctor path can resolve
  a knowledge-base MCP connection and obtain a bearer token

## Coverage notes

- IQ rules validate Search index schema, semantic configuration, vector
  profiles, embedding-model dimensions, access-control declarations, and
  schedule bounds.
- Gateway rules validate JWT policy presence, token-governance policy shape,
  backend reference integrity, path uniqueness, managed-identity forwarding,
  and telemetry sink wiring.
- External APIM `xml-link` / `rawxml-link` policies are treated as unresolved
  metadata and never counted as pass.

## Sample

See `samples/iq-gateway/` for a minimal Phase 8 fixture stub used to explain
the expected repository shape for IQ MCP connections and APIM-backed projects.

## Unverified

- `azure.ai.connection` targets can point either at a knowledge-base MCP
  endpoint (`https://{search}.search.windows.net/knowledgebases/{kb}/mcp`) or
  at a Cognitive Search service root (`https://{search}.search.windows.net`).
  Only the MCP target participates in the live knowledge-base snapshot reads.
- `FND-IQ-009` verifies the documented
  `x-ms-query-source-authorization` header for indexed and remote-SharePoint
  retrieval paths only. `x-ms-query-work-iq-source-authorization` is treated
  as Work IQ-specific and never counts as pass evidence for the indexed-source
  checks until Microsoft Learn documents that use.
