import { app, HttpRequest, HttpResponseInit, InvocationContext } from '@azure/functions';
import { loadAllSnapshots, resolveEffectivePrice } from '../lib/cosmos';
import { calculateCost } from '../lib/costCalculator';
import { EnrichedUsageMetricRecord, UsageMetricRecord } from '../lib/types';

/**
 * Called once per llm-usage-ingestion Logic App run (see the
 * Enrich_With_Pricing action added to
 * src/usage-ingestion-logicapp/llm-usage-ingestion/workflow.json), not
 * once per record. Loads the pricing catalog into memory a single time,
 * then resolves + prices every record in the batch against it — a batch
 * of a few hundred usage records costs exactly one Cosmos round trip for
 * pricing data, regardless of batch size.
 */
export async function enrichPricing(
  request: HttpRequest,
  context: InvocationContext
): Promise<HttpResponseInit> {
  let records: UsageMetricRecord[];
  try {
    records = (await request.json()) as UsageMetricRecord[];
  } catch {
    return { status: 400, jsonBody: { error: 'Request body must be a JSON array of usage records' } };
  }

  if (!Array.isArray(records)) {
    return { status: 400, jsonBody: { error: 'Request body must be a JSON array of usage records' } };
  }

  const catalog = await loadAllSnapshots();

  let unresolvedCount = 0;
  const enriched: EnrichedUsageMetricRecord[] = records.map((record) => {
    const price = resolveEffectivePrice(catalog, record.deploymentName, record.timestamp);
    if (!price) {
      unresolvedCount += 1;
    }
    return { ...record, cost: calculateCost(record, price) };
  });

  if (unresolvedCount > 0) {
    // Don't fail the batch over a missing price entry (an ingestion outage
    // is worse than a temporarily-uncosted record) — but this is exactly
    // the signal an alert should fire on. See guides/cost-attribution-guide.md,
    // "operational notes".
    context.warn(
      `enrichPricing: ${unresolvedCount}/${records.length} record(s) had no matching price snapshot for their model/timestamp.`
    );
  }

  return { jsonBody: enriched };
}

app.http('enrichPricing', {
  methods: ['POST'],
  authLevel: 'function',
  handler: enrichPricing,
});
