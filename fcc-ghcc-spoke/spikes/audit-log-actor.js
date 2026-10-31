'use strict';

/*
 * Spike: does the enterprise audit log record billing changes, with the actor
 * and how soon? Gates Phase 3 (X2 shows the actor on a drift record).
 *
 * Creates, updates and deletes a cost center and a budget through the API, then
 * polls GET /enterprises/{enterprise}/audit-log (up to SPIKE_AUDIT_WAIT_SECONDS,
 * default 120) for events newer than the start of the run that mention the
 * objects. The audit log can lag by minutes, and the token needs audit log read
 * access, so "nothing found" is reported as inconclusive unless the endpoint
 * itself was refused.
 *
 * The UI case matters more than the API case for drift (admins edit in the UI).
 * After this run, make one budget edit in the GitHub UI and rerun with
 * SPIKE_AUDIT_ONLY=yes to look at the log without making new changes.
 */

var harness = require('./_lib');
var lib = require('../lib');

function budgetOf(body) {
    return (body && body.budget) || body;
}

function mentions(event, terms) {
    var text = JSON.stringify(event);
    var i;
    for (i = 0; i < terms.length; i++) {
        if (terms[i] && text.indexOf(terms[i]) !== -1) {
            return true;
        }
    }
    return false;
}

async function run(env) {
    var ctx = harness.createContext(env);
    var auditOnly = ctx.optional('SPIKE_AUDIT_ONLY', 'no') === 'yes';
    var waitSeconds = parseInt(ctx.optional('SPIKE_AUDIT_WAIT_SECONDS', '120'), 10);
    var intervalMs = parseInt(ctx.optional('SPIKE_AUDIT_INTERVAL_MS', '15000'), 10);
    var paths = lib.endpoints.paths;
    var startedAt = Date.now() - 60 * 1000;
    var terms = [];
    var events = [];
    var waited = 0;
    var logRefused = null;
    var cc, r, id, relevant, raw;

    try {
        if (!auditOnly) {
            cc = await ctx.createCostCenter(ctx.unique('audit'));
            terms.push(cc.name, cc.id);
            await ctx.call('rename the cost center', 'PATCH', paths.costCenter(ctx.enterprise, cc.id), { name: cc.name + '-renamed' });
            r = await ctx.call('create a budget', 'POST', paths.budgets(ctx.enterprise),
                { budget_scope: 'cost_center', budget_entity_name: cc.name + '-renamed', budget_amount: 10, prevent_further_usage: true });
            if (r.ok) {
                id = (budgetOf(r.body) || {}).id;
                terms.push(id);
                await ctx.call('update the budget', 'PATCH', paths.budget(ctx.enterprise, id), { budget_amount: 20 });
                await ctx.call('delete the budget', 'DELETE', paths.budget(ctx.enterprise, id));
            }
        }
    } finally {
        await ctx.cleanup();
    }

    while (true) {
        r = await ctx.call('read the audit log (waited ' + waited + 's)', 'GET',
            '/enterprises/' + encodeURIComponent(ctx.enterprise) + '/audit-log?per_page=100&order=desc');
        if (!r.ok) {
            logRefused = 'HTTP ' + r.status + ' ' + (r.body && r.body.message);
            break;
        }
        raw = Array.isArray(r.body) ? r.body : [];
        events = raw.filter(function (e) {
            var t = e['@timestamp'] || e.created_at || 0;
            return t >= startedAt && (auditOnly || mentions(e, terms));
        });
        if (events.length || waited >= waitSeconds) {
            break;
        }
        await ctx.sleep(intervalMs);
        waited += intervalMs / 1000;
    }

    relevant = events.map(function (e) {
        return { action: e.action, actor: e.actor || e.actor_login || null, timestamp: e['@timestamp'] || e.created_at };
    });
    var details = { waitedSeconds: waited, events: relevant, auditOnly: auditOnly };

    if (logRefused) {
        details.refused = logRefused;
        return ctx.finish('audit-log-actor', 'rejected',
            'The audit log endpoint refused the request (' + logRefused + '). The token may lack audit log access.', details);
    }
    if (relevant.length) {
        return ctx.finish('audit-log-actor', 'accepted',
            relevant.length + ' relevant event(s) found after ' + waited + 's; first action "' + relevant[0].action + '", actor "' + relevant[0].actor + '".', details);
    }
    return ctx.finish('audit-log-actor', 'inconclusive',
        'No matching events within ' + waited + 's. The log may lag, or billing changes may not be recorded. Rerun with SPIKE_AUDIT_ONLY=yes later.', details);
}

module.exports = { run: run };

if (require.main === module) {
    harness.main(run);
}
