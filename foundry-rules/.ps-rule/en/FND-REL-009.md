# FND-REL-009

## SYNOPSIS

APIM availability tier recommendation

## DESCRIPTION

An API Management instance used as the AI gateway in front of Foundry should use a tier that offers an SLA and the availability features the workload needs. Documented facts: the Developer tier is for non-production use and has no SLA; availability zones and multi-region deployment are Premium features (zones also on v2 tiers per the zones article); Consumption, Basic and Standard have neither. Choosing a tier is a cost-versus-resilience decision, so this is a recommendation.

## RECOMMENDATION

Select the least APIM tier and capacity that satisfy the workload's declared SLA, throughput, zone-resilience and multi-region objectives; do not default to Premium or two units. For production traffic that requires an SLA, do not use Developer. If zone resilience is declared, select the least documented zone-capable tier and capacity that satisfy the objective in a region where APIM zone support has been verified. Select Premium and add another region only when regional resilience is declared (see FND-REL-007). Record the cost-versus-resilience tradeoff and accept a lower tier or capacity when it satisfies the declared objective.

## EXAMPLES

```
// Values are selected from the declared SLA, throughput, zone and region objectives.
// Do not hard-code Premium/2 as a universal baseline. Verify current tier SLA,
// regional feature availability and required capacity before setting these parameters.
param apimTier string
param apimCapacity int
resource apim 'Microsoft.ApiManagement/service@2024-05-01' = {
  name: apimName
  location: location
  sku: {
    name: apimTier
    capacity: apimCapacity
  }
  properties: {
    publisherEmail: publisherEmail
    publisherName: publisherName
  }
}
```

## NOTES

- Severity dev/test/prod: info/info/warning (PSRule: Information/Information/Warning).
- Catalogue status: product-opinion. Catalogue decision: adapt.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Evaluated only against a resilience objective declared in the profile; without it the catalogue skips the rule.
- How to feed it: A PSRule configuration value could carry it; not implemented.
- This rule never reports Pass in PSRule.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- Related PSRule for Azure rules from the catalogue research: Azure.APIM.AvailabilityZone, Azure.APIM.MultiRegion.

## LINKS

- [https://learn.microsoft.com/azure/api-management/api-management-features](https://learn.microsoft.com/azure/api-management/api-management-features) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/enable-availability-zone-support](https://learn.microsoft.com/azure/api-management/enable-availability-zone-support) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement/stable/2024-05-01/apimdeployment.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement/stable/2024-05-01/apimdeployment.json) (last verified 2026-10-04)
- [https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/ApiManagement/service/recommendations.yaml](https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/ApiManagement/service/recommendations.yaml) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.APIM.AvailabilityZone/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.APIM.AvailabilityZone/) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.APIM.MultiRegion/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.APIM.MultiRegion/) (last verified 2026-10-04)
