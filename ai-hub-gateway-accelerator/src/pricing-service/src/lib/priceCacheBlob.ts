import { BlobServiceClient, ContainerClient } from '@azure/storage-blob';
import { DefaultAzureCredential } from '@azure/identity';
import { PriceSnapshot } from './types';

/**
 * Exported (not just internal), same DI-seam pattern as
 * quota-service/src/lib/email.ts's getTransporter() — `BlobServiceClient`
 * construction, like nodemailer's createTransport()/Cosmos's
 * CosmosClient, is lazy and doesn't make a network call until an actual
 * operation (createIfNotExists/upload below) runs, so this can be called
 * directly in tests to exercise its own env-var-driven construction
 * logic (the missing-account-URL error) without a live storage account.
 */
export function getContainerClient(): ContainerClient {
  const accountUrl = process.env.PricingCache_StorageAccountUrl;
  if (!accountUrl) {
    throw new Error('PricingCache_StorageAccountUrl app setting is required');
  }
  const containerName = process.env.PricingCache_ContainerName ?? 'pricing-cache';
  const blobService = new BlobServiceClient(accountUrl, new DefaultAzureCredential());
  return blobService.getContainerClient(containerName);
}

export interface WriteCurrentPricingCacheDeps {
  getContainerClient: typeof getContainerClient;
}

const defaultDeps: WriteCurrentPricingCacheDeps = {
  getContainerClient,
};

/**
 * Writes the small "current prices" JSON that the customer-facing cost
 * price page reads. One cheap blob read per page load — never a live
 * Cosmos query — refreshed once a day by refreshPricingCache.
 *
 * `deps` follows the same optional, defaulted pattern as every other
 * DI seam in this fork (this session's own code-quality review) —
 * production callers never pass a second argument.
 */
export async function writeCurrentPricingCache(
  currentPrices: PriceSnapshot[],
  deps: WriteCurrentPricingCacheDeps = defaultDeps
): Promise<void> {
  const blobName = process.env.PricingCache_BlobName ?? 'current-pricing.json';

  const container = deps.getContainerClient();
  await container.createIfNotExists({ access: 'blob' }); // public-read on the blob only, not the account

  const payload = JSON.stringify(
    {
      generatedAt: new Date().toISOString(),
      prices: currentPrices.map((p) => ({
        modelFamily: p.modelFamily,
        // Deprecated alias, kept one transition period for any
        // already-built customer price page still reading `model` —
        // see guides/cost-attribution-guide.md's migration note. Remove
        // once your price page reads `modelFamily` instead.
        model: p.modelFamily,
        deploymentName: p.deploymentName,
        currency: p.Currency,
        calculationMethod: p.CalculationMethod,
        costPerInputUnit: p.CostPerInputUnit,
        costPerOutputUnit: p.CostPerOutputUnit,
        costPerCachedInputUnit: p.CostPerCachedInputUnit,
        costUnit: p.CostUnit,
        priceVersion: p.priceVersion,
        effectiveFrom: p.effectiveFrom,
      })),
    },
    null,
    2
  );

  const blockBlob = container.getBlockBlobClient(blobName);
  await blockBlob.upload(payload, Buffer.byteLength(payload), {
    blobHTTPHeaders: {
      blobContentType: 'application/json',
      // Page can poll for the daily refresh instead of always hard-caching;
      // 1h client cache is a reasonable default for a value that only ever
      // changes once every 24h.
      blobCacheControl: 'public, max-age=3600',
    },
  });
}
