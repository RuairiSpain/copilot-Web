# FND-IDN-006

## SYNOPSIS

Federated credentials use HTTPS issuer and specific subject

## DESCRIPTION

A federated identity credential on a user-assigned managed identity trusts tokens from an external identity provider (for example GitHub Actions or an AKS cluster) by matching issuer, subject and audience. The issuer must be an https URL that matches the iss claim, and the subject must name one specific workload. A standard federated credential matches the subject exactly: a wildcard or pattern in the subject does not match several workloads, it matches none. Name-based GitHub and GitLab subjects are mutable and can be recycled, so the immutable subject format should be preferred where the issuer offers it. The rule inspects Microsoft.ManagedIdentity/userAssignedIdentities/federatedIdentityCredentials in the template.

## RECOMMENDATION

Use the exact https issuer of the provider and a subject that names one workload: for GitHub Actions an environment, branch, pull request or tag of one repository, preferably in the immutable subject form; for AKS the exact namespace and service account. Do not put wildcards in a standard credential; if one credential must cover several branches, use a flexible federated identity credential (preview) with an immutable claim, and remove credentials whose workload no longer exists.

## EXAMPLES

```
resource fic 'Microsoft.ManagedIdentity/userAssignedIdentities/federatedIdentityCredentials@2024-11-30' = {
  parent: deployIdentity
  name: 'github-prod'
  properties: {
    issuer: 'https://token.actions.githubusercontent.com'
    subject: 'repo:contoso/payments-api:environment:prod' // one repository and one environment
    audiences: [ 'api://AzureADTokenExchange' ]
  }
}
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Issuer is an https URL, subject is non-empty and has no * or ? wildcard. GitHub and AKS subject formats, audiences, duplicates and the 20-credential limit are not evaluated.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://openid.net/specs/openid-connect-discovery-1_0.html#IssuerDiscovery](https://openid.net/specs/openid-connect-discovery-1_0.html#IssuerDiscovery) (last verified 2026-10-04)
- [https://learn.microsoft.com/entra/workload-id/workload-identity-federation-create-trust-user-assigned-managed-identity](https://learn.microsoft.com/entra/workload-id/workload-identity-federation-create-trust-user-assigned-managed-identity) (last verified 2026-10-04)
- [https://learn.microsoft.com/entra/workload-id/workload-identity-federation-considerations](https://learn.microsoft.com/entra/workload-id/workload-identity-federation-considerations) (last verified 2026-10-04)
- [https://learn.microsoft.com/entra/workload-id/workload-identities-federated-credential-mutable-subjects](https://learn.microsoft.com/entra/workload-id/workload-identities-federated-credential-mutable-subjects) (last verified 2026-10-04)
- [https://learn.microsoft.com/entra/workload-id/workload-identities-github-immutable-subjects](https://learn.microsoft.com/entra/workload-id/workload-identities-github-immutable-subjects) (last verified 2026-10-04)
- [https://learn.microsoft.com/entra/workload-id/workload-identities-flexible-federated-identity-credentials](https://learn.microsoft.com/entra/workload-id/workload-identities-flexible-federated-identity-credentials) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/msi/resource-manager/Microsoft.ManagedIdentity/ManagedIdentity/stable/2024-11-30/ManagedIdentity.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/msi/resource-manager/Microsoft.ManagedIdentity/ManagedIdentity/stable/2024-11-30/ManagedIdentity.json) (last verified 2026-10-04)
