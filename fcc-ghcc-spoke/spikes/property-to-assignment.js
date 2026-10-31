'use strict';

/*
 * Spike: does setting the cost_center custom property on a repo assign the repo
 * to the cost center of that name, or must the spoke call add resources?
 * Gates Phase 5. Needs SPIKE_ORG and SPIKE_REPO (repo name inside that org).
 * The token also needs organization admin rights to define and set properties.
 *
 * Optional: SPIKE_POLL_SECONDS (default 30) is how long to watch for an automatic assignment.
 */

var harness = require('./_lib');
var lib = require('../lib');

var PROPERTY = 'cost_center';
var POLL_INTERVAL_MS = 5000;

function hasRepo(resources, org, repo) {
    var i, r;
    for (i = 0; resources && i < resources.length; i++) {
        r = resources[i];
        if (r.type === 'Repo' && (r.name === repo || r.name === org + '/' + repo)) {
            return true;
        }
    }
    return false;
}

async function run(env) {
    var ctx = harness.createContext(env);
    var org = ctx.need('SPIKE_ORG');
    var repo = ctx.need('SPIKE_REPO');
    var pollSeconds = parseInt(ctx.optional('SPIKE_POLL_SECONDS', '30'), 10);
    var paths = lib.endpoints.paths;
    var propsPath = '/orgs/' + encodeURIComponent(org) + '/properties/values';
    var automatic = false;
    var manualWorks = null;
    var waited = 0;
    var r, cc, original, i;

    try {
        r = await ctx.call('read current property values', 'GET',
            '/repos/' + encodeURIComponent(org) + '/' + encodeURIComponent(repo) + '/properties/values');
        original = null;
        for (i = 0; r.ok && Array.isArray(r.body) && i < r.body.length; i++) {
            if (r.body[i].property_name === PROPERTY) {
                original = r.body[i].value;
            }
        }

        await ctx.call('ensure property is defined', 'PUT',
            '/orgs/' + encodeURIComponent(org) + '/properties/schema/' + PROPERTY, { value_type: 'string', required: false });
        ctx.note('The property definition is left in place; delete it by hand if it did not exist before this run.');

        cc = await ctx.createCostCenter(ctx.unique('property'));

        ctx.addCleanup('restore the original property value', function () {
            return ctx.call('cleanup: restore property', 'PATCH', propsPath, {
                repository_names: [repo],
                properties: [{ property_name: PROPERTY, value: original }]
            });
        });
        r = await ctx.call('set property to the cost center name', 'PATCH', propsPath, {
            repository_names: [repo],
            properties: [{ property_name: PROPERTY, value: cc.name }]
        });
        if (!r.ok) {
            throw new Error('could not set the property: HTTP ' + r.status + ' ' + JSON.stringify(r.body));
        }

        while (true) {
            r = await ctx.call('look for the repo in the cost center (waited ' + waited + 's)', 'GET', paths.costCenter(ctx.enterprise, cc.id));
            if (r.ok && hasRepo(r.body.resources, org, repo)) {
                automatic = true;
                break;
            }
            if (waited >= pollSeconds) {
                break;
            }
            await ctx.sleep(POLL_INTERVAL_MS);
            waited += POLL_INTERVAL_MS / 1000;
        }

        if (!automatic) {
            r = await ctx.call('add the repo explicitly', 'POST', paths.costCenterResource(ctx.enterprise, cc.id),
                { repositories: [org + '/' + repo] });
            if (r.ok) {
                r = await ctx.call('read back after explicit add', 'GET', paths.costCenter(ctx.enterprise, cc.id));
                manualWorks = hasRepo(r.body.resources, org, repo);
            } else {
                manualWorks = false;
            }
        }
    } finally {
        await ctx.cleanup();
    }

    if (automatic) {
        return ctx.finish('property-to-assignment', 'accepted',
            'Setting the property assigned the repo within ' + waited + 's. The spoke can set the property and verify the assignment.',
            { automatic: true, waitedSeconds: waited });
    }
    return ctx.finish('property-to-assignment', 'rejected',
        'The property alone did not assign the repo after ' + waited + 's. ' +
        (manualWorks ? 'Explicit add resources worked, so the spoke must call it.' : 'Explicit add resources also failed: read the steps.'),
        { automatic: false, waitedSeconds: waited, explicitAddWorked: manualWorks });
}

module.exports = { run: run };

if (require.main === module) {
    harness.main(run);
}
