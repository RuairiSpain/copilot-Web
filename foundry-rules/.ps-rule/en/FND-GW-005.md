# FND-GW-005

## SYNOPSIS

Gateway calls Foundry using managed identity

## DESCRIPTION

Where an API Management API forwards to a Foundry or Azure OpenAI endpoint, the gateway should authenticate with its managed identity instead of a stored key: the effective inbound policy uses authentication-managed-identity with resource https://cognitiveservices.azure.com, does not set an api-key header, the API Management service has a system-assigned or user-assigned identity, and that identity holds a Cognitive Services role on the Foundry resource.

## RECOMMENDATION

Enable a managed identity on the API Management service, assign it a Cognitive Services role on the Foundry resource, and add authentication-managed-identity for https://cognitiveservices.azure.com to the inbound policy instead of forwarding an api-key.

## EXAMPLES

```
<inbound>
  <base />
  <authentication-managed-identity resource="https://cognitiveservices.azure.com"
      output-token-variable-name="managed-id-access-token" ignore-error="false" />
  <set-header name="Authorization" exists-action="override">
    <value>@("Bearer " + (string)context.Variables["managed-id-access-token"])</value>
  </set-header>
</inbound>
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs policy XML (authentication-managed-identity, set-header) plus service identity and role assignment correlation.
- How to feed it: None implemented.
- This rule never reports Pass in PSRule.
- Related PSRule for Azure rules from the catalogue research: Azure.APIM.ManagedIdentity.

## LINKS

- [https://learn.microsoft.com/azure/api-management/api-management-authenticate-authorize-ai-apis](https://learn.microsoft.com/azure/api-management/api-management-authenticate-authorize-ai-apis) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/authentication-managed-identity-policy](https://learn.microsoft.com/azure/api-management/authentication-managed-identity-policy) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/backends](https://learn.microsoft.com/azure/api-management/backends) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/azure-ai-foundry-api](https://learn.microsoft.com/azure/api-management/azure-ai-foundry-api) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement) (last verified 2026-10-04)
