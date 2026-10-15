# Foundry Doctor threat model

Scope: the Foundry Doctor CLI, azd extension, adapters, CI/release pipeline. Foundry Doctor is a community/independent tool.

## Assets
- User repositories and `azure.yaml`/Bicep content (may reference secrets).
- Azure credentials and tokens used during `--preflight`/`--runtime`.
- Release artefacts and the azd registry entry (user trust).
- Reports (console/JSON/markdown/SARIF) that may be uploaded or shared.

## Trust boundaries and threats

| # | Threat | Mitigation |
|---|---|---|
| T1 | Doctor mutates user resources or files | Read-only guarantee: no write/delete Azure calls; what-if only; reports go only to `--out`. Tests assert no mutating verbs. |
| T2 | Secrets leak into reports, logs or SARIF | Central redaction; never emit secret values; fixtures contain no secret-shaped strings; secret scan in CI. |
| T3 | Malicious fork PR exfiltrates secrets or abuses tokens | Offline workflow needs no secrets; read-only `contents`; SARIF upload skipped for forks; Azure workflows are never triggered by `pull_request`. |
| T4 | Long-lived Azure credentials stolen | OIDC federated credentials only, scoped to protected environments with required reviewers; identifiers are variables, not secrets. |
| T5 | Compromised third-party action | All actions pinned to commit SHAs; Dependabot/dependency review; least-privilege `permissions` per job. |
| T6 | Tampered release binaries | SHA-256 checksums, keyless cosign signatures, GitHub build-provenance and SBOM attestations; release job gated by environment. |
| T7 | Namespace squatting or registry confusion in azd | ADR-003: extension id is never `microsoft.foundry`; CI check fails if upstream registries use namespace `foundry`/`foundry.*`; docs state community status. |
| T8 | Malicious or buggy adapter (Bicep, PSRule, scanners) | Adapters run as subprocesses with minimal env, allow-listed binaries, version pinning; output treated as untrusted and re-validated. |
| T9 | Path traversal via `azure.yaml` project paths | Paths resolved and constrained to the project root; symlinks not followed outside it. |
| T10 | LLM data flow exposes code or secrets | Any LLM-assisted feature is opt-in, sends only redacted minimal excerpts, and is disabled by default. |
| T11 | "Skipped" misread as "passed" | Skipped checks are reported distinctly; `--strict` returns exit 3. |

## Residual risks
- Self-hosted runners for VNet access are user-managed.
- azd custom registries have no upstream signature enforcement; users must verify cosign bundles and attestations manually.
- Redaction is pattern-based and may miss novel secret formats.

## Review
Re-review on any new adapter, workflow permission change or LLM feature.
