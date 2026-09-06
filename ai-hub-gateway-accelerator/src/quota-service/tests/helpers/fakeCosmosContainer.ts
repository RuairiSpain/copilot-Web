// An in-memory fake standing in for @azure/cosmos's Container, scoped
// EXACTLY to the subset of that API this service actually calls:
// item(id, pk).read()/.replace()/.delete(), items.create(),
// items.upsert(), items.query({query, parameters}).fetchAll().
//
// This is NOT a general Cosmos SQL emulator. Its query() method
// recognizes the small, finite set of exact query strings this codebase
// actually issues (there are five) and applies the equivalent JS filter/
// sort against the in-memory documents — it throws for any query text it
// doesn't recognize, deliberately, so a change to a production query
// string fails the test loudly (forcing this fake to be updated to
// match) rather than silently returning wrong results. That's the honest
// tradeoff of a hand-rolled fake instead of a real Cosmos account: it
// tests "does the production code send the right filter parameters and
// get correctly-filtered results back", not "is this exact SQL string
// valid Cosmos SQL" — the latter still needs real integration testing
// against a live account, same disclosure as everywhere else in this
// service's test suite.

interface StoredDoc {
  id: string;
  subscriptionId: string;
  [key: string]: unknown;
}

class CosmosLikeError extends Error {
  code: number;
  constructor(code: number, message: string) {
    super(message);
    this.code = code;
  }
}

function docKey(id: string, partitionKey: string): string {
  return `${partitionKey}::${id}`;
}

export class FakeCosmosContainer {
  private docs = new Map<string, StoredDoc>();

  /** Test setup — seed documents directly, bypassing items.create(). */
  seed(docs: StoredDoc[]): void {
    for (const doc of docs) {
      this.docs.set(docKey(doc.id, doc.subscriptionId), structuredClone(doc));
    }
  }

  /** Test assertion — inspect what's actually stored, unfiltered. */
  all(): StoredDoc[] {
    return [...this.docs.values()].map((d) => structuredClone(d));
  }

  item(id: string, partitionKey: string) {
    const key = docKey(id, partitionKey);
    return {
      read: async <T>(): Promise<{ resource: T | undefined }> => {
        const doc = this.docs.get(key);
        if (!doc) {
          // Matches @azure/cosmos's real behavior: .read() throws for a
          // missing item, it does not return an empty resource. See
          // src/lib/cosmos.ts's readItemOrUndefined() doc comment for
          // why this distinction is load-bearing (a real bug it fixed).
          throw new CosmosLikeError(404, `Fake: no document ${key}`);
        }
        return { resource: structuredClone(doc) as T };
      },
      replace: async (body: StoredDoc): Promise<void> => {
        if (!this.docs.has(key)) {
          throw new CosmosLikeError(404, `Fake: cannot replace missing document ${key}`);
        }
        this.docs.set(key, structuredClone(body));
      },
      delete: async (): Promise<void> => {
        if (!this.docs.has(key)) {
          throw new CosmosLikeError(404, `Fake: cannot delete missing document ${key}`);
        }
        this.docs.delete(key);
      },
    };
  }

  items = {
    create: async (body: StoredDoc): Promise<void> => {
      this.docs.set(docKey(body.id, body.subscriptionId), structuredClone(body));
    },
    upsert: async (body: StoredDoc): Promise<void> => {
      this.docs.set(docKey(body.id, body.subscriptionId), structuredClone(body));
    },
    query: <T>(spec: { query: string; parameters?: { name: string; value: unknown }[] }) => {
      const params = new Map((spec.parameters ?? []).map((p) => [p.name, p.value]));
      const all = [...this.docs.values()];
      const results = matchKnownQuery(spec.query, params, all);
      return {
        fetchAll: async (): Promise<{ resources: T[] }> => ({ resources: results.map((d) => structuredClone(d)) as T[] }),
      };
    },
  };
}

function matchKnownQuery(query: string, params: Map<string, unknown>, docs: StoredDoc[]): StoredDoc[] {
  const q = query.replace(/\s+/g, ' ').trim();

  // submitQuotaRequest.ts — one-pending-request-per-scope guardrail.
  if (q.startsWith('SELECT c.id FROM c WHERE c.subscriptionId = @subscriptionId')) {
    return docs.filter(
      (d) =>
        d.subscriptionId === params.get('@subscriptionId') &&
        d.scopeType === params.get('@scopeType') &&
        d.scopeId === params.get('@scopeId') &&
        d.status === 'Pending'
    );
  }

  // listPendingQuotaRequests.ts
  if (q.startsWith('SELECT * FROM c WHERE c.status = "Pending" AND NOT IS_DEFINED(c.notifiedAt)')) {
    return docs
      .filter((d) => d.status === 'Pending' && d.notifiedAt === undefined)
      .sort((a, b) => {
        if (a.requiresEscalation !== b.requiresEscalation) return a.requiresEscalation ? -1 : 1;
        return String(a.createdAt).localeCompare(String(b.createdAt));
      });
  }

  // listRecentlyDecidedQuotaRequests.ts
  if (q.startsWith('SELECT * FROM c WHERE (c.status = "Approved" OR c.status = "Denied")')) {
    return docs
      .filter((d) => (d.status === 'Approved' || d.status === 'Denied') && d.requestedByEmail !== undefined && d.requesterNotifiedAt === undefined)
      .sort((a, b) => String(a.createdAt).localeCompare(String(b.createdAt)));
  }

  // expireQuotaOverrides.ts
  if (q.startsWith('SELECT c.id, c.subscriptionId FROM c WHERE c.expiresAt != null')) {
    const now = String(params.get('@now'));
    return docs.filter((d) => d.expiresAt !== null && d.expiresAt !== undefined && String(d.expiresAt) <= now);
  }

  // resetMonthlyQuotaOverrides.ts — select-all, filtering happens in JS
  // via survivesMonthlyReset() in the production code itself.
  if (q === 'SELECT c.id, c.subscriptionId, c.expiresAt FROM c') {
    return docs;
  }

  throw new Error(
    `FakeCosmosContainer: unrecognized query — "${query}". ` +
      'This fake only interprets the known, exact query strings this service issues; ' +
      'update matchKnownQuery() if production code introduced a new one.'
  );
}
