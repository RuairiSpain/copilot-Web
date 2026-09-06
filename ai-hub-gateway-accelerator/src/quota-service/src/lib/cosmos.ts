import { CosmosClient, Container } from '@azure/cosmos';
import { DefaultAzureCredential } from '@azure/identity';

let cachedClient: CosmosClient | undefined;

/**
 * Managed-identity Cosmos client — same pattern as
 * src/pricing-service/src/lib/cosmos.ts (no connection strings/keys in
 * app settings).
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

function database() {
  const databaseId = process.env.CosmosDB_Database ?? 'ai-usage-db';
  return getClient().database(databaseId);
}

export function getQuotaOverridesContainer(): Container {
  const containerId = process.env.CosmosDB_QuotaOverridesContainer ?? 'quota-overrides';
  return database().container(containerId);
}

export function getQuotaOverrideRequestsContainer(): Container {
  const containerId = process.env.CosmosDB_QuotaOverrideRequestsContainer ?? 'quota-override-requests';
  return database().container(containerId);
}

/**
 * @azure/cosmos's `container.item(id, pk).read()` throws (not "returns
 * an empty resource") for a missing item — confirmed by the SDK's own
 * documented behavior and already correctly handled this way in
 * getQuotaAllowance.ts. This wraps that once, correctly, so every
 * call site gets the same safe "undefined for 404, rethrow anything
 * else" contract without re-deriving it.
 *
 * BUG FIX: prior to this, decideQuotaRequest.ts, markQuotaRequestNotified.ts,
 * and markRequesterNotified.ts called `.read()` directly with no
 * try/catch, then checked `if (!quotaRequest)` — dead code, since a
 * missing item throws rather than returning a falsy resource. A request
 * for a genuinely nonexistent id/subscriptionId would have produced an
 * unhandled exception (Azure Functions returning a raw 500) instead of
 * the intended, documented 404 JSON response. Caught while adding real
 * test coverage against a faithful fake Cosmos container — exactly the
 * kind of bug that class of testing exists to catch.
 */
export async function readItemOrUndefined<T extends object>(container: Container, id: string, partitionKey: string): Promise<T | undefined> {
  try {
    const { resource } = await container.item(id, partitionKey).read<T>();
    return resource;
  } catch (err) {
    const statusCode = (err as { code?: number })?.code;
    if (statusCode === 404) {
      return undefined;
    }
    throw err;
  }
}

/**
 * TEST-ONLY. Clears the cached CosmosClient so tests/cosmos.test.ts can
 * exercise getClient()'s env-var-driven construction repeatedly
 * (including its missing-endpoint error) instead of getting whichever
 * client the first test in the process happened to construct.
 */
export function _resetClientCacheForTests(): void {
  cachedClient = undefined;
}
