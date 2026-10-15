---
name: prepare-release
description: Prepare and verify a Foundry Doctor release, including compatibility, changelog, binaries, checksums, SBOM, signatures, provenance, and extension manifest. Use only when explicitly asked to rehearse or cut a release.
---

# Prepare a Foundry Doctor release

1. Confirm the intended branch is clean and CI is green.
2. Run the `run-acceptance-gates` skill.
3. Verify compatibility metadata, deprecations, migration notes, and minimum supported azd, Bicep, and Foundry extension versions.
4. Use `devops-release` to produce the changelog, regenerated catalogue, supported binaries, checksums, SBOM, provenance, signatures, and extension manifest.
5. Install and smoke-test from release artifacts rather than the source tree.
6. Obtain independent `foundry-lead` and `azure-black-belt` approval.
7. Do not publish with unresolved compatibility or security findings.

Publishing, tagging, pushing, or releasing artifacts requires explicit user approval.
