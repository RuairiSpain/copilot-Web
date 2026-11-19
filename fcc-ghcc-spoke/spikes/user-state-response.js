'use strict';

/*
 * Spike: what does "get user states for a multi-user budget" return, and at which path?
 * Gates Phase 4 (blocked-user detection, B6). Needs SPIKE_USER.
 *
 * Creates a multi-user budget on a cost center holding SPIKE_USER, then tries the
 * likely paths. The first 2xx response is saved as the sample the mock should copy.
 * A freshly created budget may have no spend yet, so the sample may show an empty or
 * minimal user list; rerun after the user has used Copilot to see blocked states.
 */

var harness = require('./_lib');
var lib = require('../lib');

function budgetOf(body) {
    return (body && body.budget) || body;
}

async function run(env) {
    var ctx = harness.createContext(env);
    var user = ctx.need('SPIKE_USER');
    var paths = lib.endpoints.paths;
    var candidates, cc, r, id, i, hit = null, tried = [];

    try {
        cc = await ctx.createCostCenter(ctx.unique('userstates'));
        await ctx.call('add user to the cost center', 'POST', paths.costCenterResource(ctx.enterprise, cc.id), { users: [user] });
        r = await ctx.call('create multi-user budget', 'POST', paths.budgets(ctx.enterprise), {
            budget_scope: 'multi_user', budget_entity_name: cc.name, budget_amount: 5, prevent_further_usage: true
        });
        if (!r.ok) {
            throw new Error('could not create a multi-user budget: HTTP ' + r.status + ' ' + JSON.stringify(r.body));
        }
        id = (budgetOf(r.body) || {}).id;
        ctx.addCleanup('delete the multi-user budget', function () {
            return ctx.call('cleanup: delete budget', 'DELETE', paths.budget(ctx.enterprise, id));
        });

        candidates = [
            paths.budgetUserStates(ctx.enterprise, id),
            paths.budget(ctx.enterprise, id) + '/user_states',
            paths.budget(ctx.enterprise, id) + '/users'
        ];
        for (i = 0; i < candidates.length && !hit; i++) {
            r = await ctx.call('get user states at ' + candidates[i], 'GET', candidates[i]);
            tried.push({ path: candidates[i], status: r.status });
            if (r.ok) {
                hit = { path: candidates[i], sample: r.body };
            }
        }
    } finally {
        await ctx.cleanup();
    }

    if (hit) {
        return ctx.finish('user-state-response', 'accepted',
            'User states are served at ' + hit.path + '. Copy the sample from details into the mock and lib/endpoints.js.',
            { path: hit.path, sample: hit.sample, tried: tried });
    }
    return ctx.finish('user-state-response', 'rejected',
        'None of the candidate paths returned user states. Check the endpoint name in the REST docs (a community report mentions a 404).',
        { tried: tried });
}

module.exports = { run: run };

if (require.main === module) {
    harness.main(run);
}
