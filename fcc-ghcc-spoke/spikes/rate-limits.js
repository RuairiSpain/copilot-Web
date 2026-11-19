'use strict';

/*
 * Spike: what rate limit applies to the billing endpoints, and what does an
 * hourly budget poll cost against it? Gates Phase 3 (X6). Read-only.
 *
 * Calls the budgets list in a loop (SPIKE_RATE_CALLS, default 30), recording the
 * x-ratelimit headers on each response, and stops at the first rate-limit
 * response. It does not try to exhaust a large limit: if the limit is not reached,
 * the finding gives the observed limit and the cost per call, which is enough to
 * size the poll.
 */

var harness = require('./_lib');
var lib = require('../lib');

function num(v) {
    var n = parseInt(v, 10);
    return isNaN(n) ? null : n;
}

async function run(env) {
    var ctx = harness.createContext(env);
    var calls = parseInt(ctx.optional('SPIKE_RATE_CALLS', '30'), 10);
    var paths = lib.endpoints.paths;
    var samples = [];
    var hitLimit = null;
    var i, r, remaining;

    for (i = 0; i < calls; i++) {
        r = await ctx.call('list budgets, call ' + (i + 1), 'GET', paths.budgets(ctx.enterprise) + '?per_page=100');
        remaining = num(r.headers['x-ratelimit-remaining']);
        samples.push({ status: r.status, limit: num(r.headers['x-ratelimit-limit']), remaining: remaining, resource: r.headers['x-ratelimit-resource'] || null });
        if (r.status === 429 || (r.status === 403 && (remaining === 0 || r.headers['retry-after']))) {
            hitLimit = { atCall: i + 1, status: r.status, retryAfter: r.headers['retry-after'] || null, reset: r.headers['x-ratelimit-reset'] || null };
            break;
        }
        if (!r.ok) {
            break;
        }
    }

    var first = samples[0] || {};
    var last = samples[samples.length - 1] || {};
    var measured = samples.filter(function (s) { return s.status < 300; });
    var costPerCall = null;
    if (measured.length > 1 && measured[0].remaining !== null && measured[measured.length - 1].remaining !== null) {
        costPerCall = (measured[0].remaining - measured[measured.length - 1].remaining) / (measured.length - 1);
    }
    var details = {
        callsMade: samples.length,
        limit: first.limit,
        resource: first.resource,
        remainingAfter: last.remaining,
        costPerCall: costPerCall,
        hitLimit: hitLimit
    };

    if (hitLimit) {
        return ctx.finish('rate-limits', 'accepted',
            'Rate limit reached at call ' + hitLimit.atCall + ' (HTTP ' + hitLimit.status + '). Limit ' + first.limit + ', retry-after ' + hitLimit.retryAfter + '.',
            details);
    }
    if (first.limit !== null && first.limit !== undefined) {
        return ctx.finish('rate-limits', 'accepted',
            'Limit ' + first.limit + ' on resource "' + first.resource + '", about ' + costPerCall + ' per call. Limit not reached in ' + samples.length + ' calls.',
            details);
    }
    return ctx.finish('rate-limits', 'inconclusive',
        'No x-ratelimit headers were returned on these endpoints. Treat 429 and retry-after as the only signal.', details);
}

module.exports = { run: run };

if (require.main === module) {
    harness.main(run);
}
