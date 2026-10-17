# FND-IQ-011

## SYNOPSIS

Refresh schedule valid and supportable

## DESCRIPTION

An indexed knowledge source ingestionSchedule (and any generated or user indexer schedule) is an IndexingSchedule: interval is required, must be an XSD dayTimeDuration of the form P(nD)(T(nH)(nM)), between 5 minutes (PT5M) and 1440 minutes (24 hours); startTime, when set, is a UTC timestamp. The schedule must also be supportable: the data source needs change detection, and a single run is limited to the indexer maximum running time of the tier.

## RECOMMENDATION

Use an interval between PT5M and P1D, for example PT2H, and make sure the data source has change detection so scheduled runs resume where the previous one stopped.

## EXAMPLES

```
"ingestionSchedule": { "interval": "PT2H", "startTime": "2026-01-01T00:00:00Z" }
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Search data-plane read of ingestionSchedule and indexer schedules.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://learn.microsoft.com/azure/search/search-howto-schedule-indexers](https://learn.microsoft.com/azure/search/search-howto-schedule-indexers) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/search-limits-quotas-capacity](https://learn.microsoft.com/azure/search/search-limits-quotas-capacity) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/agentic-knowledge-source-how-to-blob](https://learn.microsoft.com/azure/search/agentic-knowledge-source-how-to-blob) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/search/data-plane/Search](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/search/data-plane/Search) (last verified 2026-10-04)
