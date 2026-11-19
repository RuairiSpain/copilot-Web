'use strict';

/*
 * Spike: does a GitHub App installation token work on the billing endpoints?
 * Informs the single-PAT risk. Needs SPIKE_APP_TOKEN, an installation token you
 * minted for an app installed on the throwaway enterprise (the script does not
 * handle app keys). GITHUB_TOKEN is still required, for the harness and cleanup.
 *
 * Reads are tried first. The write check creates and deletes one cost center
 * with the app token; if the create fails, nothing is left behind.
 */

var harness = require('./_lib');
var lib = require('../lib');

async function run(env) {
    var ctx = harness.createContext(env);
    var appToken = ctx.need('SPIKE_APP_TOKEN');
    var paths = lib.endpoints.paths;
    var results = {};
    var name = ctx.unique('app');
    var r, id;

    r = await ctx.call('list cost centers with the app token', 'GET', paths.costCenters(ctx.enterprise), undefined, { token: appToken });
    results.listCostCenters = r.status;
    r = await ctx.call('list budgets with the app token', 'GET', paths.budgets(ctx.enterprise) + '?per_page=1', undefined, { token: appToken });
    results.listBudgets = r.status;

    r = await ctx.call('create a cost center with the app token', 'POST', paths.costCenters(ctx.enterprise), { name: name }, { token: appToken });
    results.createCostCenter = r.status;
    if (r.ok) {
        id = r.body.id;
        r = await ctx.call('delete that cost center with the app token', 'DELETE', paths.costCenter(ctx.enterprise, id), undefined, { token: appToken });
        results.deleteCostCenter = r.status;
        if (!r.ok) {
            await ctx.call('cleanup: delete with the main token', 'DELETE', paths.costCenter(ctx.enterprise, id));
        }
    }

    var readsOk = results.listCostCenters < 300 && results.listBudgets < 300;
    var writesOk = results.createCostCenter < 300;
    var details = { results: results };

    if (readsOk && writesOk) {
        return ctx.finish('github-app-auth', 'accepted',
            'The app token can read and write cost centers and read budgets. A GitHub App can replace the PAT for these endpoints.', details);
    }
    if (readsOk) {
        return ctx.finish('github-app-auth', 'inconclusive',
            'The app token can read but not write cost centers (create returned HTTP ' + results.createCostCenter + '). A PAT is still needed for writes.', details);
    }
    return ctx.finish('github-app-auth', 'rejected',
        'The app token was refused (list cost centers HTTP ' + results.listCostCenters + ', list budgets HTTP ' + results.listBudgets + '). The classic PAT stays.', details);
}

module.exports = { run: run };

if (require.main === module) {
    harness.main(run);
}
