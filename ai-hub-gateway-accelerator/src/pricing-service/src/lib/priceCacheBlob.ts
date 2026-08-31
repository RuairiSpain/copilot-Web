import { BlobServiceClient } from '@azure/storage-blob';
import { DefaultAzureCredential } from '@azure/identity';
import { PriceSnapshot } from './types';

/**
 * Writes the small "current prices" JSON that the customer-facing cost
 * price page reads. One cheap blob read per page load — never a live
 * Cosmos query — refreshed once a day by refreshPricingCache.
 */
export async function writeCurrentPricingCache(currentPrices: PriceSnapshot[]): Promise<void> {
  const accountUrl = process.env.PricingCache_StorageAccountUrl;
  if (!accountUrl) {
    throw new Error('PricingCache_StorageAccountUrl app setting is required');
  }
  const containerName = process.env.PricingCache_ContainerName ?? 'pricing-cache';
  const blobName = process.env.PricingCache_BlobName ?? 'current-pricing.json';

  const blobService = new BlobServiceClient(accountUrl, new DefaultAzureCredential());
  const container = blobService.getContainerClient(containerName);
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
