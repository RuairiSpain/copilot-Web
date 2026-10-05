# Foundry Doctor samples

Fixtures for exercising the offline doctor. All values are fake; none contain
secrets or secret-shaped strings.

| Sample | Purpose | Expected result |
|---|---|---|
| `good/` | Valid `azure.yaml` with project, deployment and hosted agent | No findings |
| `bad/` | Duplicate `name:` key, unresolved `${EXAMPLE_MISSING_VAR}`, `uses:` typo | Findings (exit 1) |
| `azure-yaml-only/` | `azure.yaml` with no infra directory | Infra checks reported as skipped, not passed |
| `bicep-backed/` | `azure.yaml` plus `infra/main.bicep` and a module (Bicep 0.48.1 valid) | Bicep analysed; no findings expected |

Each sample's `agents/assistant/` directory holds a `.gitkeep` placeholder.
