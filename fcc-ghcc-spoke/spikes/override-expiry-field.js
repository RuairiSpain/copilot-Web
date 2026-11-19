'use strict';

/*
 * Spike: does a GitHub budget have an expiry field, so overrides could expire
 * natively (B4)? Gates Phase 4. Needs SPIKE_USER.
 *
 * REST gives no schema, so each candidate field is sent on a user budget and the
 * budget is read back: a field counts only if it comes back with the value sent.
 * A field GitHub silently ignores is reported as ignored. Also check the OpenAPI
 * description of the budgets endpoints by hand; this spike cannot see fields it
 * does not guess.
 */

var harness = require('./_lib');
var lib = require('../lib');

var CANDIDATES = ['expires_at', 'expiry', 'expiration_date', 'end_date', 'budget_expires_at'];

function budgetOf(body) {
    return (body && body.budget) || body;
}

async function run(env) {
    var ctx = harness.createContext(env);
    var user = ctx.need('SPIKE_USER');
    var paths = lib.endpoints.paths;
    var expiry = new Date(Date.now() + 7 * 24 * 3600 * 1000).toISOString();
    var created = [];
    var results = {};
    var persistedField = null;
    var i, body, r, got, id;

    try {
        ctx.addCleanup('delete budgets created by this run', async function () {
            var j;
            for (j = created.length - 1; j >= 0; j--) {
                await ctx.call('cleanup: delete budget ' + created[j], 'DELETE', paths.budget(ctx.enterprise, created[j]));
            }
        });
        for (i = 0; i < CANDIDATES.length; i++) {
            body = { budget_scope: 'user', user: user, budget_amount: 5, prevent_further_usage: true };
            body[CANDIDATES[i]] = expiry;
            r = await ctx.call('create user budget with ' + CANDIDATES[i], 'POST', paths.budgets(ctx.enterprise), body);
            if (!r.ok) {
                results[CANDIDATES[i]] = 'rejected: HTTP ' + r.status + ' ' + (r.body && r.body.message);
                continue;
            }
            id = (budgetOf(r.body) || {}).id;
            created.push(id);
            r = await ctx.call('read back budget with ' + CANDIDATES[i], 'GET', paths.budget(ctx.enterprise, id));
            got = budgetOf(r.body) || {};
            if (got[CANDIDATES[i]] !== undefined && String(got[CANDIDATES[i]]).indexOf(expiry.slice(0, 10)) === 0) {
                results[CANDIDATES[i]] = 'stored';
                persistedField = persistedField || CANDIDATES[i];
            } else {
                results[CANDIDATES[i]] = 'accepted but ignored';
            }
            /* The next candidate would collide with this budget, so remove it first. */
            await ctx.call('delete budget before next candidate', 'DELETE', paths.budget(ctx.enterprise, id));
            created.pop();
        }
    } finally {
        await ctx.cleanup();
    }

    if (persistedField) {
        return ctx.finish('override-expiry-field', 'accepted',
            'Field "' + persistedField + '" came back with the value sent. Confirm it is enforced, not only stored, before relying on it.',
            { field: persistedField, results: results });
    }
    return ctx.finish('override-expiry-field', 'rejected',
        'No candidate field was stored. ServiceNow owns override expiry (B4 fallback). Also check the OpenAPI description by hand.',
        { results: results });
}

module.exports = { run: run };

if (require.main === module) {
    harness.main(run);
}
