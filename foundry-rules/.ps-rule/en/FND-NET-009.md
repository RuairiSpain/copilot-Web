# FND-NET-009

## SYNOPSIS

APIM SKU supports required private backend connectivity

## DESCRIPTION

When Azure API Management fronts Foundry (AI gateway pattern) in a network-isolated design, its tier must support the private connectivity the design needs. Documented support: classic VNet injection (external or internal) on Developer and Premium; Premium v2 VNet injection; outbound VNet integration to reach private backends on Standard v2 and Premium v2; inbound private endpoint on Developer, Basic, Standard, Standard v2, Premium and Premium v2. The Consumption tier and Basic v2 are not listed for any of these models. The rule compares the APIM sku.name with the connectivity the template configures or the design declares, and fails on a combination the documentation does not support.

## RECOMMENDATION

Choose the tier for the connectivity model: Standard v2 or Premium v2 for outbound integration to a private backend, Premium v2 or classic Premium/Developer for VNet injection, and any tier in the inbound private endpoint list for private clients. Do not plan on Consumption for private networking.

## EXAMPLES

```
resource apim 'Microsoft.ApiManagement/service@2025-09-01-preview' = {
  name: apimName
  location: location
  sku: { name: 'StandardV2', capacity: 1 } // outbound VNet integration is documented for Standard v2 and Premium v2
  properties: {
    publisherEmail: publisherEmail
    publisherName: publisherName
    // then configure the delegated integration subnet as described in the API Management VNet integration article
  }
}
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: virtualNetworkType External or Internal requires sku.name Developer or Premium. The V2 delegated-subnet and private-endpoint models are not evaluated; the catalogue marks the ARM properties for them as unverified.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/api-management/virtual-network-concepts](https://learn.microsoft.com/azure/api-management/virtual-network-concepts) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/private-endpoint](https://learn.microsoft.com/azure/api-management/private-endpoint) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/integrate-vnet-outbound](https://learn.microsoft.com/azure/api-management/integrate-vnet-outbound) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/how-to/ai-gateway](https://learn.microsoft.com/azure/foundry/agents/how-to/ai-gateway) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement/preview/2025-09-01-preview/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement/preview/2025-09-01-preview/openapi.json) (last verified 2026-10-04)
