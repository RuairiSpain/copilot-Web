'use strict';

/*
 * Spike: with an org in one cost center and one of its repos in another, are both
 * assignments stored, and does adding the repo move or remove anything?
 * Gates Phase 5. Needs SPIKE_ORG and SPIKE_REPO (a repo name inside that org).
 *
 * As with org-as-resource, the stored resources do not say which cost center is
 * billed; the finding lists the manual usage check.
 */

var harness = require('./_lib');
var lib = require('../lib');

var MANUAL_STEP = 'To learn which assignment wins for billing: run a Copilot cloud agent or Actions job in SPIKE_REPO, then run the ' +
    'usage report filtered by each cost center and see which one shows the usage.';

function names(resources) {
    var out = [];
    var i;
    for (i = 0; resources && i < resources.length; i++) {
        out.push(resources[i].type + ':' + resources[i].name);
    }
    return out;
}

async function run(env) {
    var ctx = harness.createContext(env);
    var org = ctx.need('SPIKE_ORG');
    var repo = ctx.need('SPIKE_REPO');
    var paths = lib.endpoints.paths;
    var fullName = org + '/' + repo;
    var details = {};
    var a, b, r, readA, readB, i, found;

    try {
        a = await ctx.createCostCenter(ctx.unique('org-side'));
        b = await ctx.createCostCenter(ctx.unique('repo-side'));

        r = await ctx.call('add org to cost center A', 'POST', paths.costCenterResource(ctx.enterprise, a.id), { organizations: [org] });
        details.orgAccepted = r.ok;
        r = await ctx.call('add repo to cost center B', 'POST', paths.costCenterResource(ctx.enterprise, b.id), { repositories: [fullName] });
        details.repoAccepted = r.ok;
        details.repoReassigned = r.body && r.body.reassigned_resources;

        readA = await ctx.call('read back A', 'GET', paths.costCenter(ctx.enterprise, a.id));
        readB = await ctx.call('read back B', 'GET', paths.costCenter(ctx.enterprise, b.id));
        details.resourcesA = names(readA.body && readA.body.resources);
        details.resourcesB = names(readB.body && readB.body.resources);

        found = false;
        for (i = 0; i < details.resourcesB.length; i++) {
            if (/^Repo:/.test(details.resourcesB[i]) && details.resourcesB[i].indexOf(repo) !== -1) {
                found = true;
            }
        }
        details.repoStoredInB = found;
        details.orgStoredInA = details.resourcesA.indexOf('Org:' + org) !== -1;
    } finally {
        await ctx.cleanup();
    }
    details.manualStep = MANUAL_STEP;

    if (details.orgStoredInA && details.repoStoredInB) {
        return ctx.finish('org-vs-repo-precedence', 'accepted',
            'Both assignments were stored (org in A, repo in B) and neither moved the other. Which one bills needs the manual usage check.', details);
    }
    return ctx.finish('org-vs-repo-precedence', 'inconclusive',
        'The two assignments did not both stay in place. Read the steps and resourcesA / resourcesB.', details);
}

module.exports = { run: run };

if (require.main === module) {
    harness.main(run);
}
