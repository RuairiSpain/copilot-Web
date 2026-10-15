# FND-DEP-009

## SYNOPSIS

Likely policy denials identified

## DESCRIPTION

Before deployment, ask Azure Policy what restrictions it would place on the planned resources at the target scope, and list the policies whose evaluation is non-compliant or that restrict a field the template sets. The result is advisory: Azure Policy evaluates the resource content it is given, so unresolved template expressions and effects that act after creation cannot be settled in advance.

## RECOMMENDATION

Change the planned value to one the policy allows, request a policy exemption, or ask the policy owner for a compliant pattern. Do not disable the policy to make a deployment pass.

## EXAMPLES

```
# example: a policy restricts locations; pick an allowed one
azd env set AZURE_LOCATION <allowed-region>
```

## NOTES

- Severity dev/test/prod: warning/warning/warning (PSRule: Warning/Warning/Warning).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Microsoft.PolicyInsights checkPolicyRestrictions, or the policy assignment list.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/policyinsights/resource-manager/Microsoft.PolicyInsights/PolicyInsights/stable/2024-10-01/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/policyinsights/resource-manager/Microsoft.PolicyInsights/PolicyInsights/stable/2024-10-01/openapi.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Authorization/policy/stable/2026-07-01/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Authorization/policy/stable/2026-07-01/openapi.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/permissions/management-and-governance](https://learn.microsoft.com/azure/role-based-access-control/permissions/management-and-governance) (last verified 2026-10-04)
- [https://github.com/Azure/azure-policy/blob/master/built-in-policies/policyDefinitions/General/AllowedLocations_Deny.json](https://github.com/Azure/azure-policy/blob/master/built-in-policies/policyDefinitions/General/AllowedLocations_Deny.json) (last verified 2026-10-04)
