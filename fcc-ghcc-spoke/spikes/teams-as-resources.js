'use strict';

/*
 * Spike: can an enterprise team be a cost center resource?
 * Gates Phase 2 (funding teams). Needs SPIKE_TEAM, a team slug in the throwaway enterprise.
 *
 * Tries the likely request keys in turn, then reads the cost center back to see
 * what GitHub stored (the team itself, or its members).
 */

var harness = require('./_lib');
var lib = require('../lib');

var CANDIDATE_KEYS = ['teams', 'enterprise_teams'];

async function run(env) {
    var ctx = harness.createContext(env);
    var team = ctx.need('SPIKE_TEAM');
    var paths = lib.endpoints.paths;
    var accepted = null;
    var firstRejection = null;
    var readback = null;
    var i, body, r;

    try {
        var cc = await ctx.createCostCenter(ctx.unique('teams'));
        for (i = 0; i < CANDIDATE_KEYS.length && !accepted; i++) {
            body = {};
            body[CANDIDATE_KEYS[i]] = [team];
            r = await ctx.call('add team using key "' + CANDIDATE_KEYS[i] + '"', 'POST', paths.costCenterResource(ctx.enterprise, cc.id), body);
            if (r.ok) {
                accepted = CANDIDATE_KEYS[i];
            } else if (!firstRejection) {
                firstRejection = 'HTTP ' + r.status + ' ' + (r.body && r.body.message);
            }
        }
        r = await ctx.call('read back cost center', 'GET', paths.costCenter(ctx.enterprise, cc.id));
        readback = r.body && r.body.resources;
    } finally {
        await ctx.cleanup();
    }

    if (accepted) {
        return ctx.finish('teams-as-resources', 'accepted',
            'Accepted using key "' + accepted + '". Check "resources" in the findings: if it lists members instead of the team, ' +
            'R2 must be checked against membership.',
            { key: accepted, resourcesAfter: readback });
    }
    return ctx.finish('teams-as-resources', 'rejected',
        'No candidate key was accepted (' + firstRejection + '). Use the fallback: expand teams to direct user resources.',
        { triedKeys: CANDIDATE_KEYS });
}

module.exports = { run: run };

if (require.main === module) {
    harness.main(run);
}
