# FND-GW-001

## SYNOPSIS

JWT validates issuer, audience and tenant

## DESCRIPTION

Inbound policy of an AI gateway API must validate the caller's token with validate-jwt or validate-azure-ad-token such that the issuer, the audience and the tenant are all constrained: validate-jwt needs an audiences list with at least one audience, an issuer source (issuers element or an openid-config endpoint that supplies it), and a tenant restriction (tenant-specific openid-config URL, issuer containing the tenant ID, or a tid required-claim); validate-azure-ad-token needs a tenant-id that is a specific tenant (not organizations or common) and an audience or application ID constraint.

## RECOMMENDATION

Validate the token at the gateway before forwarding to the model: pin the tenant (tenant-specific openid-config URL or validate-azure-ad-token tenant-id), list the expected audience, and keep signed-token and expiry requirements at their secure defaults.

## EXAMPLES

```
<inbound>
  <base />
  <validate-azure-ad-token tenant-id="{{tenant-id}}">
    <audiences>
      <audience>api://foundry-gateway</audience>
    </audiences>
    <client-application-ids>
      <application-id>{{client-app-id}}</application-id>
    </client-application-ids>
  </validate-azure-ad-token>
</inbound>
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs API Management policy XML (validate-jwt, validate-azure-ad-token) resolved with <base /> inheritance.
- How to feed it: None implemented.
- This rule never reports Pass in PSRule.

## LINKS

- [https://learn.microsoft.com/azure/api-management/validate-jwt-policy](https://learn.microsoft.com/azure/api-management/validate-jwt-policy) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/validate-azure-ad-token-policy](https://learn.microsoft.com/azure/api-management/validate-azure-ad-token-policy) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/api-management-authenticate-authorize-ai-apis](https://learn.microsoft.com/azure/api-management/api-management-authenticate-authorize-ai-apis) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement) (last verified 2026-10-04)
