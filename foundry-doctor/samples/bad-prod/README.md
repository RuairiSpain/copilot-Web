# bad-prod

Intentionally defective sample for the `foundry-prod` profile (the default) at azd 1.34.2.
`infra/main.json` is `infra/main.bicep` compiled with Bicep CLI 0.48.1 (`bicep build main.bicep --outfile main.json --no-restore`).
All values are fake. `SEARCH_API_KEY` holds a made-up non-secret string.

| Rule | Defect | Expected finding |
|---|---|---|
| FND-IDN-002 | The only role assignment is an unrelated Reader for a group; the project system-assigned identity has no role on Search, Storage or Cosmos DB | error, five findings (Search Index Data Contributor, Search Service Contributor, Storage Blob Data Contributor, Storage Blob Data Owner, Cosmos DB Operator) |
| FND-OPS-002 | No connection of category `AppInsights` on the project or account | warning |
| FND-REL-004 | No `CanNotDelete` lock on the Foundry account, Search, Storage or Cosmos DB | warning x4 |
| FND-REL-008 | `ProvisionedManaged` deployment `chat-ptu` has no `spilloverDeploymentName` | warning |
| FND-SEC-007 | `raiPolicyName` is `undeclared-strict-policy`, which is not declared as a `raiPolicies` resource | error |
| FND-SEC-010 | `.foundry-doctor/config.yaml` sets `policy.encryption.requireCustomerManagedKeys: true`; nothing uses a customer-managed key | info x4 |
| FND-CFG-003 | `uses: [ai-projct]` names no service | error |
| FND-CFG-005 | `SEARCH_API_KEY` is a credential-named env key holding a literal | error |
| FND-CFG-007 | `TOOL_ENDPOINT` is an `http://` URL | error |
| FND-CFG-012 | Search `location` is `eastus-typo` | error |

The sample also trips older rules (SEC-001..004, NET-001, OPS-001, OPS-003 and others); they are not the point of this sample.
FND-SEC-007 on the azure.yaml side is skipped at azd 1.34.2 (the hosted-agent `policies` field first exists in 1.36.0-beta.1), so only the ARM leg fires.
