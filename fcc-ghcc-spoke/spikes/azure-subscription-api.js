'use strict';

/*
 * Spike: can an Azure subscription be set on a cost center through the API?
 * Gates Phase 2 (the PRD ties each org's overspend to an Azure subscription).
 *
 * Tries the likely field names on create and on update, then reads the cost center
 * back. Set SPIKE_AZURE_SUBSCRIPTION to a subscription ID that is linked to the
 * throwaway enterprise; without it a placeholder is sent, and a rejection may mean
 * the placeholder is invalid rather than the field being unsupported, which the
 * finding states.
 */

var harness = require('./_lib');
var lib = require('../lib');

var CANDIDATES = ['azure_subscription', 'azure_subscription_id'];
var PLACEHOLDER = '00000000-0000-0000-0000-000000000000';

async function run(env) {
    var ctx = harness.createContext(env);
    var subscription = ctx.optional('SPIKE_AZURE_SUBSCRIPTION', PLACEHOLDER);
    var usedPlaceholder = subscription === PLACEHOLDER;
    var paths = lib.endpoints.paths;
    var results = {};
    var stored = null;
    var cc, body, r, i, value;

    try {
        cc = await ctx.createCostCenter(ctx.unique('azure'));
        r = await ctx.call('read the new cost center', 'GET', paths.costCenter(ctx.enterprise, cc.id));
        results.fieldPresentOnRead = !!r.body && Object.prototype.hasOwnProperty.call(r.body, 'azure_subscription');

        /* Stop at the first field that is stored; a later field would otherwise read the earlier one's value back. */
        for (i = 0; i < CANDIDATES.length && !stored; i++) {
            body = {};
            body[CANDIDATES[i]] = subscription;
            r = await ctx.call('update cost center with ' + CANDIDATES[i], 'PATCH', paths.costCenter(ctx.enterprise, cc.id), body);
            if (!r.ok) {
                results[CANDIDATES[i]] = 'rejected: HTTP ' + r.status + ' ' + (r.body && r.body.message);
                continue;
            }
            r = await ctx.call('read back after ' + CANDIDATES[i], 'GET', paths.costCenter(ctx.enterprise, cc.id));
            value = r.body && (r.body.azure_subscription || r.body.azure_subscription_id);
            if (value === subscription) {
                results[CANDIDATES[i]] = 'stored';
                stored = stored || CANDIDATES[i];
            } else {
                results[CANDIDATES[i]] = 'accepted but not stored';
            }
        }
    } finally {
        await ctx.cleanup();
    }

    if (stored) {
        return ctx.finish('azure-subscription-api', 'accepted',
            'Field "' + stored + '" was stored on the cost center. Add it to the cost_center sync and the plan.',
            { field: stored, usedPlaceholder: usedPlaceholder, results: results });
    }
    return ctx.finish('azure-subscription-api', usedPlaceholder ? 'inconclusive' : 'rejected',
        usedPlaceholder
            ? 'Nothing was stored, but a placeholder subscription was used, so this does not show the field is unsupported. Rerun with SPIKE_AZURE_SUBSCRIPTION.'
            : 'Nothing was stored with a real subscription ID. Keep the Azure subscription on org only and manage the link in GitHub billing settings.',
        { usedPlaceholder: usedPlaceholder, results: results });
}

module.exports = { run: run };

if (require.main === module) {
    harness.main(run);
}
