'use strict';

/*
 * Spike: can an org be a cost center resource (the basis of the UNALLOCATED-<org>
 * fallback in R1), and does a finer assignment coexist with it?
 * Gates Phase 2. Needs SPIKE_ORG and SPIKE_USER (a member of that org).
 *
 * Puts the org in cost center A, then the user in cost center B, and reads both
 * back. Which cost center a charge is billed to cannot be read from the
 * resources alone; the finding lists the manual check.
 */

var harness = require('./_lib');
var lib = require('../lib');

var MANUAL_STEP = 'To learn which assignment wins for billing: have SPIKE_USER consume some Copilot usage, then run the AI credit ' +
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
    var user = ctx.need('SPIKE_USER');
    var paths = lib.endpoints.paths;
    var details = {};
    var a, b, r, afterOrg, afterUser;

    try {
        a = await ctx.createCostCenter(ctx.unique('org-fallback'));
        b = await ctx.createCostCenter(ctx.unique('org-user'));

        r = await ctx.call('add org to cost center A', 'POST', paths.costCenterResource(ctx.enterprise, a.id), { organizations: [org] });
        details.orgAccepted = r.ok;
        if (!r.ok) {
            details.orgResponse = 'HTTP ' + r.status + ' ' + (r.body && r.body.message);
        } else {
            r = await ctx.call('add user from the same org to cost center B', 'POST', paths.costCenterResource(ctx.enterprise, b.id), { users: [user] });
            details.userAccepted = r.ok;
            details.userReassigned = r.body && r.body.reassigned_resources;
            afterOrg = await ctx.call('read back A', 'GET', paths.costCenter(ctx.enterprise, a.id));
            afterUser = await ctx.call('read back B', 'GET', paths.costCenter(ctx.enterprise, b.id));
            details.resourcesA = names(afterOrg.body && afterOrg.body.resources);
            details.resourcesB = names(afterUser.body && afterUser.body.resources);
            details.coexist = details.resourcesA.indexOf('Org:' + org) !== -1 && details.resourcesB.indexOf('User:' + user) !== -1;
        }
    } finally {
        await ctx.cleanup();
    }
    details.manualStep = MANUAL_STEP;

    if (!details.orgAccepted) {
        return ctx.finish('org-as-resource', 'rejected',
            'An org was refused as a cost center resource (' + details.orgResponse + '). R1 needs another fallback design.', details);
    }
    if (details.coexist) {
        return ctx.finish('org-as-resource', 'accepted',
            'The org was accepted and a user from it could sit in another cost center; both assignments were stored. ' +
            'Which one bills still needs the manual usage check.', details);
    }
    return ctx.finish('org-as-resource', 'inconclusive',
        'The org was accepted, but the finer assignment did not coexist with it. Read the steps.', details);
}

module.exports = { run: run };

if (require.main === module) {
    harness.main(run);
}
