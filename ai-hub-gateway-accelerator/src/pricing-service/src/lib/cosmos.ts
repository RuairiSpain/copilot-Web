import { CosmosClient, Container } from '@azure/cosmos';
import { DefaultAzureCredential } from '@azure/identity';
import { PriceSnapshot, CurrentPricePointer } from './types';

let cachedClient: CosmosClient | undefined;

/**
 * Managed-identity Cosmos client, matching how the rest of this accelerator
 * authenticates to Azure resources (no connection strings/keys in app
 * settings — see NFR-005-equivalent guidance in guides/cost-attribution-guide.md).
 */
function getClient(): CosmosClient {
  if (!cachedClient) {
    const endpoint = process.env.CosmosDB_Endpoint;
    if (!endpoint) {
      throw new Error('CosmosDB_Endpoint app setting is required');
    }
    cachedClient = new CosmosClient({
      endpoint,
      aadCredentials: new DefaultAzureCredential(),
    });
  }
  return cachedClient;
}

export function getPricingContainer(): Container {
  const databaseId = process.env.CosmosDB_Database ?? 'ai-usage-db';
  const containerId = process.env.CosmosDB_PricingContainer ?? 'model-pricing';
  return getClient().database(databaseId).container(containerId);
}

/**
 * Loads the full pricing catalog (all snapshots, all models) into memory
 * once — used by both Functions so a batch of N usage records costs one
 * Cosmos round trip, not N.
 */
export async function loadAllSnapshots(): Promise<PriceSnapshot[]> {
  const container = getPricingContainer();
  const { resources } = await container.items
    .query<PriceSnapshot>({
      query: "SELECT * FROM c WHERE c.docType = 'priceSnapshot'",
    })
    .fetchAll();
  return resources;
}

export async function loadCurrentPointers(): Promise<CurrentPricePointer[]> {
  const container = getPricingContainer();
  const { resources } = await container.items
    .query<CurrentPricePointer>({
      query: "SELECT * FROM c WHERE c.docType = 'currentPricePointer'",
    })
    .fetchAll();
  return resources;
}

/**
 * Resolves the price snapshot effective at a given point in time for a
 * deployment, from an already-loaded in-memory catalog (no per-record
 * Cosmos read — see enrichPricing.ts).
 *
 * Matches on `deploymentName` — the actual Azure deployment name a usage
 * record carries — never on `modelFamily` (display-only). This used to
 * match on a field literally called `model` that, in practice, had to
 * hold the deployment name anyway (every seed entry set `model` and
 * `deploymentName` to the same string); renaming that field to
 * `modelFamily` and matching explicitly on `deploymentName` here makes
 * the real join key impossible to get wrong by accident.
 */
export function resolveEffectivePrice(
  catalog: PriceSnapshot[],
  deploymentName: string,
  atIso: string
): PriceSnapshot | undefined {
  const at = new Date(atIso).getTime();
  const candidates = catalog.filter((s) => s.deploymentName === deploymentName);
  return candidates
    .filter((s) => {
      const from = new Date(s.effectiveFrom).getTime();
      const to = s.effectiveTo ? new Date(s.effectiveTo).getTime() : Infinity;
      return from <= at && at < to;
    })
    // If more than one snapshot matches (shouldn't happen if effectiveTo is
    // maintained correctly), prefer the most recently effective one.
    .sort((a, b) => new Date(b.effectiveFrom).getTime() - new Date(a.effectiveFrom).getTime())[0];
}

export async function upsertSnapshot(snapshot: PriceSnapshot): Promise<void> {
  const container = getPricingContainer();
  await container.items.upsert(snapshot);
}

export async function upsertCurrentPointer(pointer: CurrentPricePointer): Promise<void> {
  const container = getPricingContainer();
  await container.items.upsert(pointer);
}
