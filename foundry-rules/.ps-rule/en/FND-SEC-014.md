# FND-SEC-014

## SYNOPSIS

Secrets not emitted as Bicep outputs

## DESCRIPTION

Bicep outputs must not carry secrets, because deployment outputs are stored in deployment history and, for azd, are copied into the azd environment values. The rule detects outputs derived from secure parameters, list*() functions such as listKeys, and outputs named like password.

## RECOMMENDATION

Output the resourceId of the resource that holds the secret, or store the secret in Key Vault, and read it at the consuming resource. Do not output keys, connection strings or passwords.

## EXAMPLES

```
output storageId string = stg.id // consumer calls listKeys(storageId, ...) when it needs the key; do not output keys
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: wrap.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Catalogue decision is wrap of the Bicep linter diagnostic outputs-should-not-contain-secrets. It needs the Bicep CLI diagnostics, not an ARM property.
- How to feed it: Run bicep build and read the diagnostic, or use PSRule for Azure Azure.Deployment.OutputSecretValue (listed as related by the FND-CFG-005 research; equivalence is not verified here).
- This rule never reports Pass in PSRule.

## LINKS

- [https://learn.microsoft.com/azure/azure-resource-manager/bicep/linter-rule-outputs-should-not-contain-secrets](https://learn.microsoft.com/azure/azure-resource-manager/bicep/linter-rule-outputs-should-not-contain-secrets) (last verified 2026-10-16)
- [https://learn.microsoft.com/azure/azure-resource-manager/bicep/linter](https://learn.microsoft.com/azure/azure-resource-manager/bicep/linter) (last verified 2026-10-05)
- [https://learn.microsoft.com/azure/developer/azure-developer-cli/azd-templates](https://learn.microsoft.com/azure/developer/azure-developer-cli/azd-templates) (last verified 2026-10-04)
- [https://github.com/Azure/bicep/blob/4d4395e327b32da45ae3d961aa464d28e54d26f7/src/Bicep.Core/Analyzers/Linter/Rules/OutputsShouldNotContainSecretsRule.cs](https://github.com/Azure/bicep/blob/4d4395e327b32da45ae3d961aa464d28e54d26f7/src/Bicep.Core/Analyzers/Linter/Rules/OutputsShouldNotContainSecretsRule.cs) (last verified 2026-10-05)
