# FND-REL-003

## SYNOPSIS

Cosmos zone redundancy and continuous backup

## DESCRIPTION

A Cosmos DB account used by Foundry (Agent Service standard mode state store) should have zone redundancy on its locations and continuous backup with point-in-time restore. Foundry guidance recommends continuous backup before any incident, and zone-redundant configuration. WAF/WARA recommendation, not a platform constraint.

## RECOMMENDATION

Set isZoneRedundant true on each location in regions that support availability zones, and set backupPolicy.type Continuous (7-day or 30-day tier, per the Foundry guidance) before an incident occurs. Continuous backup adds cost. Also consider a second location with automatic failover enabled (see FND-REL-007).

## EXAMPLES

```
resource cosmos 'Microsoft.DocumentDB/databaseAccounts@2025-10-15' = {
  name: cosmosName
  location: location
  properties: {
    databaseAccountOfferType: 'Standard'
    locations: [
      { locationName: location, failoverPriority: 0, isZoneRedundant: true }
    ]
    backupPolicy: {
      type: 'Continuous'
      continuousModeProperties: { tier: 'Continuous7Days' }
    }
  }
}
```

## NOTES

- Severity dev/test/prod: info/info/warning (PSRule: Information/Information/Warning).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: static-ps. Status: evaluated-subset.
- Evaluated here: Every properties.locations[].isZoneRedundant is true, and backupPolicy.type is Continuous. The continuous tier information and the region-without-zones uncertainty are not emitted.
- The catalogue notes contain unverified items; see the catalogue entry notes field.
- Related PSRule for Azure rules from the catalogue research: Azure.Cosmos.AvailabilityZone, Azure.Cosmos.ContinuousBackup.

## LINKS

- [https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency](https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2025-10-15/cosmos-db.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2025-10-15/cosmos-db.json) (last verified 2026-10-04)
- [https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/DocumentDB/databaseAccounts/recommendations.yaml](https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/DocumentDB/databaseAccounts/recommendations.yaml) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Cosmos.AvailabilityZone/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Cosmos.AvailabilityZone/) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Cosmos.ContinuousBackup/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Cosmos.ContinuousBackup/) (last verified 2026-10-04)
