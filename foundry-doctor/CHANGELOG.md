# Changelog

All notable Foundry Doctor changes are recorded here.

## Unreleased

### Added

- standalone `foundry-doctor version`
- `compare --fail-on-diff`
- azd extension entrypoint module under `extension/`
- strict v1 config schema documentation in `docs/configuration.md`
- local release rehearsal script and install scripts
- CSA demo runbook in `docs/demo/README.md`
- V1 product ADR addenda in `docs/adr/`

### Changed

- repository configuration path is finalized as `.foundry-doctor/config.yaml`
- accepted policy keys now include:
  - `policy.network.dnsManagedByPolicy`
  - `policy.network.centralDns`
  - `policy.network.cloud`
  - `policy.environments.tiers`
  - `policy.preflight.deploymentHistoryMargin`
  - `policy.preflight.regionMatrixStalenessDays`
- Azure authentication now prefers `AZURE_ACCESS_TOKEN`, then `azd`, then `az`
- release workflow signs release archives and metadata with Sigstore keyless signing

### Compatibility

- minimum verified azd compatibility floor: `1.34.2`
- minimum verified Bicep compatibility floor: `0.48.1`
