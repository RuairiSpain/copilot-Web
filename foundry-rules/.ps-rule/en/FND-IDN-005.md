# FND-IDN-005

## SYNOPSIS

Standing human admin access reviewed

## DESCRIPTION

Humans (users and groups) who hold a privileged role on the Foundry subscription, resource group, account or project as a permanent active assignment should be reviewed, and just-in-time access through Microsoft Entra Privileged Identity Management (PIM) should be preferred. The RBAC documentation recommends at most three subscription owners, avoiding privileged administrator roles where a job function role works, narrow scopes, groups instead of users, and PIM. The rule produces an inventory of standing privileged human assignments that cover Foundry resources and marks each as permanent active, time-bound or eligible when PIM data is readable. It does not decide who should have access; it makes the review input deterministic.

## RECOMMENDATION

Review each listed assignment. Prefer a job function role over a privileged administrator role, assign roles to groups, use the narrowest scope, and make human admin access eligible or time-bound through PIM where licensed. Keep between two and three subscription owners.

## EXAMPLES

```
// Make an admin group eligible through PIM instead of a permanent assignment (portal, CLI or REST):
// Access control (IAM) > Add role assignment > Assignment type: Eligible
// Time-bound active assignment through a role assignment schedule request is also acceptable.
```

## NOTES

- Severity dev/test/prod: info/info/warning (PSRule: Information/Information/Warning).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Live role assignments and PIM schedule instances at subscription, resource group, account and project scope.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/role-based-access-control/best-practices](https://learn.microsoft.com/azure/role-based-access-control/best-practices) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/pim-integration](https://learn.microsoft.com/azure/role-based-access-control/pim-integration) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/authorization/resource-manager/Microsoft.Authorization/Authorization/stable/2020-10-01/RoleAssignmentScheduleInstance.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/authorization/resource-manager/Microsoft.Authorization/Authorization/stable/2020-10-01/RoleAssignmentScheduleInstance.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/role-assignments-steps](https://learn.microsoft.com/azure/role-based-access-control/role-assignments-steps) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/concepts/rbac-foundry](https://learn.microsoft.com/azure/foundry/concepts/rbac-foundry) (last verified 2026-10-04)
