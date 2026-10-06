# ADR-015: Verified compatibility floors for azd and Bicep

Status: Accepted  
Date: 2026-10-06

## Context

Foundry Doctor needs explicit minimum compatible versions for azd and Bicep,
and those floors must be backed by evidence rather than inferred from docs.

Two local verifications were captured for V1:

- `azd version --output json` returned a nested object:
  `{"azd":{"version":"1.34.2","commit":"..."}}`
- `BICEP_PATH=C:\Users\rodonnell\bicep-v0.48.1\publish-win-arm64\bicep.exe`
  reported `Bicep CLI version 0.48.1 (...)`

## Decision

1. The minimum verified azd compatibility floor for V1 is **1.34.2**.
2. azd version parsing accepts both:
   - `{"azd":{"version":"..."}}`
   - `{"version":"..."}`
   and falls back to semantic-version extraction from plain text.
3. The minimum verified Bicep compatibility floor for V1 is **0.48.1**.
4. Older Bicep versions are never treated as compatible silently. They produce
   a clear dependency error stating the detected and supported versions.

## Consequences

- `foundry-doctor version` reports the verified azd and Bicep floors.
- release docs and compatibility docs can state a verified floor rather than a
  proposal.
- CLI behaviour is explicit when a user has an older Bicep CLI.
