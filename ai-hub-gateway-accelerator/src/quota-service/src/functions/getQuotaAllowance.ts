import { app, HttpRequest, HttpResponseInit, InvocationContext } from '@azure/functions';
import { getQuotaOverridesContainer, readItemOrUndefined } from '../lib/cosmos';
import { resolveAllowance } from '../lib/quotaLogic';
import { QuotaOverride } from '../lib/types';

export interface GetQuotaAllowanceDeps {
  getContainer: () => ReturnType<typeof getQuotaOverridesContainer>;
}

const defaultDeps: GetQuotaAllowanceDeps = {
  getContainer: getQuotaOverridesContainer,
};

/**
 * Called from the APIM policy fragment frag-load-quota-allowance.xml on a
 * cache miss only (APIM caches the result for 300s — see
 * guides/quota-override-approval.md §4) — this endpoint is on the
 * synchronous request path, so it does exactly one Cosmos point read and
 * nothing else.
 *
 * Input: { scopeType, scopeId, subscriptionId, baselineQuota }
 * baselineQuota is passed IN by the policy (it already knows the
 * contract's own tier-1 token-quota) rather than looked up here — this
 * function has no notion of which access contract maps to which quota,
 * and shouldn't need one just to answer "is there an override for this
 * scope".
 *
 * `deps` defaults to the real Cosmos container — tests inject a fake
 * (see tests/getQuotaAllowance.test.ts) so this runs against in-memory
 * data instead of a live account. Production callers (app.http below)
 * never pass a third argument, so this is a no-op change to real
 * behavior.
 */
export async function getQuotaAllowance(
  request: HttpRequest,
  context: InvocationContext,
  deps: GetQuotaAllowanceDeps = defaultDeps
): Promise<HttpResponseInit> {
  let body: { scopeType?: string; scopeId?: string; subscriptionId?: string; baselineQuota?: number };
  try {
    body = (await request.json()) as typeof body;
  } catch {
    return { status: 400, jsonBody: { error: 'Request body must be JSON' } };
  }

  const { scopeType, scopeId, subscriptionId, baselineQuota } = body;
  if (
    (scopeType !== 'user' && scopeType !== 'team') ||
    typeof scopeId !== 'string' ||
    scopeId.length === 0 ||
    typeof subscriptionId !== 'string' ||
    subscriptionId.length === 0 ||
    typeof baselineQuota !== 'number' ||
    !Number.isFinite(baselineQuota) ||
    baselineQuota < 0
  ) {
    return {
      status: 400,
      jsonBody: {
        error:
          'Request body must be { scopeType: "user"|"team", scopeId: non-empty string, subscriptionId: non-empty string, baselineQuota: number >= 0 }',
      },
    };
  }

  const id = `${scopeType}-${scopeId}`;
  const container = deps.getContainer();

  let override: QuotaOverride | undefined;
  try {
    override = await readItemOrUndefined<QuotaOverride>(container, id, subscriptionId);
  } catch (err: unknown) {
    // readItemOrUndefined already turns a 404 into `undefined` (the
    // expected common case — no override exists for this scope) and
    // only rethrows real failures. The calling policy fragment is
    // designed to fail open (fall back to the contract's own
    // baseline-only enforcement) on any non-200 from this endpoint —
    // see guides/quota-override-approval.md §Phase A step 1's explicit
    // fail-open decision — so returning 502 here is safe: it degrades
    // tier 2, it does not block traffic.
    context.error(`getQuotaAllowance: Cosmos read failed for ${id}`, err);
    return { status: 502, jsonBody: { error: 'Quota lookup failed' } };
  }

  const allowance = resolveAllowance(override, baselineQuota);
  return { jsonBody: allowance };
}

app.http('getQuotaAllowance', {
  methods: ['POST'],
  authLevel: 'function',
  handler: getQuotaAllowance,
});
