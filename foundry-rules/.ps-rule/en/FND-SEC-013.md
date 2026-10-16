# FND-SEC-013

## SYNOPSIS

Gateway tenant validation is robust

## DESCRIPTION

Token validation policies on an AI gateway must pin the Microsoft Entra tenant and the accepted client or audience. validate-azure-ad-token with tenant-id of organizations or common accepts tokens from any (or any and personal) directory per the policy reference; omitting both client-application-ids and audiences leaves the token unscoped.

## RECOMMENDATION

Set tenant-id to your tenant ID, add the expected audience and client application IDs, and keep authentication at the API level. If multi-tenant access is intended, enumerate allowed tenants through issuers and claims and record a suppression with reason and expiry.

## EXAMPLES

```
<validate-azure-ad-token tenant-id="{{aad-tenant-id}}">
  <client-application-ids>
    <application-id>{{client-app-id}}</application-id>
  </client-application-ids>
  <audiences>
    <audience>{{api-audience}}</audience>
  </audiences>
</validate-azure-ad-token>
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs API Management policy XML at service, product, API or operation scope.
- How to feed it: None implemented.
- This rule never reports Pass in PSRule.

## LINKS

- [https://learn.microsoft.com/azure/api-management/validate-azure-ad-token-policy](https://learn.microsoft.com/azure/api-management/validate-azure-ad-token-policy) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/validate-jwt-policy](https://learn.microsoft.com/azure/api-management/validate-jwt-policy) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/api-management-authenticate-authorize-ai-apis](https://learn.microsoft.com/azure/api-management/api-management-authenticate-authorize-ai-apis) (last verified 2026-10-04)
