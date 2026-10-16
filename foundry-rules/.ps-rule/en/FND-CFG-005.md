# FND-CFG-005

## SYNOPSIS

No raw secrets in YAML/Bicep/environment blocks

## DESCRIPTION

Secret values must not appear as literals in azure.yaml (azure.ai.connection credentials, service env maps, target URLs with embedded credentials or SAS tokens), in Bicep or ARM templates and parameter files (non-secure parameters, hard-coded defaults on secure parameters, secrets in outputs), or in azd environment .env files committed to source control. A value that is a ${VAR} or ${{...}} reference is not a raw secret.

## RECOMMENDATION

Store the secret in the azd environment as a secret (azd env set-secret) or in Key Vault, and reference it with ${VAR} in azure.yaml. In Bicep, mark secret parameters @secure() with no default and never output them. Prefer Entra ID or managed identity authentication where the target supports it.

## EXAMPLES

```
services:
  github-conn:
    host: azure.ai.connection
    uses: [ai-project]
    category: RemoteTool
    target: https://api.githubcopilot.com/mcp
    authType: CustomKeys
    credentials:
      Authorization: ${GITHUB_PAT}   # set with: azd env set-secret GITHUB_PAT
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs .env tracking state in git and the azure.yaml credential shapes by authType. The catalogue maps the Bicep side to other tools.
- How to feed it: For the Bicep side run PSRule for Azure Azure.Deployment.* rules and the Bicep linter (listed by the catalogue overlap research).
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.
- Related PSRule for Azure rules from the catalogue research: Azure.Deployment.SecureParameter, Azure.Deployment.SecureValue, Azure.Deployment.SecretLeak, Azure.Deployment.OutputSecretValue.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference](https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference) (last verified 2026-10-05)
- [https://learn.microsoft.com/azure/developer/azure-developer-cli/manage-environment-variables](https://learn.microsoft.com/azure/developer/azure-developer-cli/manage-environment-variables) (last verified 2026-10-05)
- [https://learn.microsoft.com/azure/developer/azure-developer-cli/environment-secrets](https://learn.microsoft.com/azure/developer/azure-developer-cli/environment-secrets) (last verified 2026-10-05)
- [https://learn.microsoft.com/azure/azure-resource-manager/bicep/linter-rule-secure-secrets-in-parameters](https://learn.microsoft.com/azure/azure-resource-manager/bicep/linter-rule-secure-secrets-in-parameters) (last verified 2026-10-04)
