# ADR-017: Release verification and azd registry metadata

Status: Accepted  
Date: 2026-10-06

## Context

V1 needs a reproducible release contract for standalone binaries and the azd
extension. GitHub Releases can publish archives and attestations, but azd
custom registries do not provide native signature enforcement for extension
metadata.

## Decision

1. Release assets include:
   - standalone archives for all supported OS/arch targets
   - azd extension archives for the same targets
   - `checksums.txt`
   - `extension.yaml`
   - `registry.json`
   - SPDX SBOM
2. Sigstore keyless signing covers:
   - each released archive
   - `checksums.txt`
   - `extension.yaml`
   - `registry.json`
   - the SBOM
3. GitHub provenance attestations remain the authoritative provenance record.
4. The azd extension registry metadata uses:
   - extension id **`ruairispain.foundry-doctor`**
   - namespace **`foundry`**
   - entrypoint **`foundry-doctor-azd`**
5. Because azd registries do not verify signatures themselves, users verify
   release bundles by checking the archive checksum and then validating the
   Sigstore bundle and GitHub attestation out of band.

## Consequences

- release automation signs both binaries and metadata, not just generated
  manifests
- extension installation docs must include manual verification guidance
- the extension id is publisher-scoped and avoids the reserved
  `microsoft.foundry` space documented in ADR-003
