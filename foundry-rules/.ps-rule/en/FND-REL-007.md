# FND-REL-007

## SYNOPSIS

Multi-region/DR plan question

## DESCRIPTION

A question rather than a check: has the team decided on a multi-region or disaster recovery strategy for the Foundry project and its stateful dependencies? Whether a region failover is needed is a business decision (RTO/RPO, cost) that cannot be derived from resource configuration. The rule emits an informational prompt, and records the answer if the project supplies one (for example a declared DR tier in the Foundry Doctor configuration). It never fails a deployment.

## RECOMMENDATION

Decide and record the intended strategy: single region with backup and restore, or multi-region (hot/hot, hot/warm, hot/cold) with the RTO/RPO and cost it implies. For multi-region Foundry the article says you create separate Foundry resources and dependencies in each region, deploy to both from CI/CD, and replicate or rebuild state (Cosmos DB read regions with service-managed failover, a second AI Search service, storage redundancy).

## EXAMPLES

```
# foundry-doctor configuration (key name as in ADR-007; the schema is defined by the configuration spec)
reliability:
  disasterRecovery:
    strategy: single-region   # or hot-hot | hot-warm | hot-cold
    recoveryRegion: northeurope
    notes: Recover from Cosmos point-in-time backup; rebuild Search index from source
```

## NOTES

- Severity dev/test/prod: info/info/info (PSRule: Information/Information/Information).
- Catalogue status: product-opinion. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: No pass or fail evidence exists; the answer is a declared disaster recovery strategy.
- How to feed it: None.
- This rule never reports Pass in PSRule.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.

## LINKS

- [https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency](https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency) (last verified 2026-10-04)
- [https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/Search/searchServices/recommendations.yaml](https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/Search/searchServices/recommendations.yaml) (last verified 2026-10-04)
- [https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/DocumentDB/databaseAccounts/recommendations.yaml](https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/DocumentDB/databaseAccounts/recommendations.yaml) (last verified 2026-10-04)
- [https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/CognitiveServices/accounts/recommendations.yaml](https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/CognitiveServices/accounts/recommendations.yaml) (last verified 2026-10-04)
