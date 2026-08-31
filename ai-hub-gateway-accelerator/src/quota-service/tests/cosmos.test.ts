import { test, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { _resetClientCacheForTests, getQuotaOverridesContainer, getQuotaOverrideRequestsContainer, readItemOrUndefined } from '../src/lib/cosmos';
import { FakeCosmosContainer } from './helpers/fakeCosmosContainer';

beforeEach(() => {
  _resetClientCacheForTests();
});

test('readItemOrUndefined: returns the document when it exists', async () => {
  const container = new FakeCosmosContainer();
  container.seed([{ id: 'req-1', subscriptionId: 'sub-1', status: 'Pending' }]);
  const doc = await readItemOrUndefined<{ id: string; status: string }>(container as never, 'req-1', 'sub-1');
  assert.equal(doc?.status, 'Pending');
});

test('readItemOrUndefined: returns undefined (not a throw) for a missing document — the actual bug fix', async () => {
  // Prior to this helper existing, decideQuotaRequest.ts/markQuotaRequestNotified.ts/
  // markRequesterNotified.ts called .read() directly with no try/catch and
  // checked `if (!quotaRequest)` — dead code, since @azure/cosmos's real
  // .read() throws (not returns falsy) for a missing item. This test is
  // exactly what would have caught that: it fails if readItemOrUndefined
  // lets the fake container's 404 throw propagate instead of translating
  // it to `undefined`.
  const container = new FakeCosmosContainer();
  const doc = await readItemOrUndefined(container as never, 'does-not-exist', 'sub-1');
  assert.equal(doc, undefined);
});

test('readItemOrUndefined: rethrows a non-404 error rather than swallowing it', async () => {
  const throwingContainer = {
    item: () => ({
      read: async () => {
        const err = new Error('service unavailable') as Error & { code: number };
        err.code = 503;
        throw err;
      },
    }),
  };
  await assert.rejects(() => readItemOrUndefined(throwingContainer as never, 'x', 'y'), /service unavailable/);
});

test('getQuotaOverridesContainer / getQuotaOverrideRequestsContainer: construct cleanly against a fake endpoint (lazy — no real connection attempted) and resolve the documented default names', () => {
  const originalEndpoint = process.env.CosmosDB_Endpoint;
  const originalDb = process.env.CosmosDB_Database;
  const originalOverridesContainer = process.env.CosmosDB_QuotaOverridesContainer;
  const originalRequestsContainer = process.env.CosmosDB_QuotaOverrideRequestsContainer;

  process.env.CosmosDB_Endpoint = 'https://fake-account.documents.azure.com:443/';
  delete process.env.CosmosDB_Database;
  delete process.env.CosmosDB_QuotaOverridesContainer;
  delete process.env.CosmosDB_QuotaOverrideRequestsContainer;

  // @azure/cosmos's CosmosClient constructor and .database()/.container()
  // calls are lazy — no network round trip until a data operation runs —
  // confirmed while implementing this test, same as jose's
  // createRemoteJWKSet and nodemailer's createTransport. Safe to call for
  // real here.
  const overridesContainer = getQuotaOverridesContainer();
  const requestsContainer = getQuotaOverrideRequestsContainer();

  assert.equal(overridesContainer.id, 'quota-overrides');
  assert.equal(requestsContainer.id, 'quota-override-requests');

  if (originalEndpoint === undefined) delete process.env.CosmosDB_Endpoint;
  else process.env.CosmosDB_Endpoint = originalEndpoint;
  if (originalDb !== undefined) process.env.CosmosDB_Database = originalDb;
  if (originalOverridesContainer !== undefined) process.env.CosmosDB_QuotaOverridesContainer = originalOverridesContainer;
  if (originalRequestsContainer !== undefined) process.env.CosmosDB_QuotaOverrideRequestsContainer = originalRequestsContainer;
});

test('getQuotaOverridesContainer: missing CosmosDB_Endpoint throws a clear error', () => {
  const original = process.env.CosmosDB_Endpoint;
  delete process.env.CosmosDB_Endpoint;
  assert.throws(() => getQuotaOverridesContainer(), /CosmosDB_Endpoint/);
  if (original !== undefined) process.env.CosmosDB_Endpoint = original;
});
