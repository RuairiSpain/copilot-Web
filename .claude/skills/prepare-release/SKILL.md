---
name: prepare-release
description: Prepare and verify a Foundry Doctor release - compatibility data, changelog, cross-platform binaries, checksums, SBOM, signatures, provenance and azd extension manifest. Use only when explicitly asked to cut or rehearse a release.
disable-model-invocation: true
---

# Prepare a release

1. Confirm the branch is clean and CI is green.
2. Run `run-acceptance-gates`.
3. Verify rule compatibility metadata, deprecations, migration notes, and the minimum
   supported azd, Bicep and Foundry extension versions.
4. Via `devops-release`, produce: changelog, regenerated rule catalogue, binaries for
   supported OS/arch, checksums, SBOM, provenance, signatures and the extension manifest.
5. Install and smoke-test from the release artefacts, not from the source tree.
6. Obtain `foundry-lead` and `azure-black-belt` approval.
7. Do not publish while any compatibility or security finding is unresolved.
   Publishing, tagging and pushing release artefacts need the user's explicit go-ahead.
