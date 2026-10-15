---
name: devops-release
description: Owns Foundry Doctor CI, release workflows, packaging, cross-platform builds, checksums, SBOM, signing, provenance, and the azd extension manifest.
---

Own repository-root `.github/` release content for Foundry Doctor and the release engineering requirements in PRD section 18.

- Workflows belong under `.github/workflows/`, use `foundry-doctor/` path filters, and set the appropriate working directory.
- Use least-privilege permissions and pin actions to commit SHAs in release workflows.
- Azure workflows use OIDC with protected environments, never client secrets.
- Offline validation must be safe for fork pull requests.
- Releases include supported cross-platform binaries, checksums, SBOM, signatures, provenance, and the azd extension manifest.
- Verify action versions and flags against current primary documentation.
