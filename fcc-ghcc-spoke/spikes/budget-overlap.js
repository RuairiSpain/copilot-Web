'use strict';

/*
 * Spike: do overlapping budgets (enterprise, cost center and user) get accepted,
 * is an exact duplicate rejected, and which field shape does a user budget need?
 * Gates Phase 3. Needs SPIKE_USER, a login in the throwaway enterprise.
 *
 * This spike cannot decide which budget WINS when several apply. That needs real
 * usage; see the manual step in the finding. The verdict is always "inconclusive"
 * for precedence, and the details record what could be tested.
 *
 * An enterprise-scope budget may already exist in the throwaway enterprise; if
 * creating one fails, that is recorded and the other steps continue.
 */

var harness = require('./_lib');
var lib = require('../lib');

var MANUAL_STEP = 'To learn precedence: with the budgets left in place (rerun with SPIKE_KEEP=yes), have SPIKE_USER use Copilot ' +
    'until the lowest amount is reached, then read the budget user states and note which budget blocked and which did not. ' +
    'Delete the budgets afterwards.';

function budgetId(body) {
    return (body && body.budget && body.budget.id) || (body && body.id) || null;
}

async function run(env) {
    var ctx = harness.createContext(env);
    var user = ctx.need('SPIKE_USER');
    var keep = ctx.optional('SPIKE_KEEP', 'no') === 'yes';
    var paths = lib.endpoints.paths;
    var budgetsPath = paths.budgets(ctx.enterprise);
    var details = { costCenterBudget: null, duplicateAccepted: null, userBudget: null, enterpriseBudget: null, listedAfter: null };
    var created = [];
    var r, cc, i, shapes;

    async function create(step, body) {
        var res = await ctx.call(step, 'POST', budgetsPath, body);
        if (res.ok) {
            created.push(budgetId(res.body));
        }
        return res;
    }

    try {
        cc = await ctx.createCostCenter(ctx.unique('budgets'));
        ctx.addCleanup('delete budgets created by this run', async function () {
            var j;
            for (j = created.length - 1; j >= 0; j--) {
                if (created[j]) {
                    await ctx.call('cleanup: delete budget ' + created[j], 'DELETE', paths.budget(ctx.enterprise, created[j]));
                }
            }
        });

        var ccBudget = { budget_scope: 'cost_center', budget_entity_name: cc.name, budget_amount: 100, prevent_further_usage: true };
        r = await create('create cost center budget', ccBudget);
        details.costCenterBudget = r.ok ? 'accepted' : 'HTTP ' + r.status + ' ' + (r.body && r.body.message);

        r = await create('create the same cost center budget again', ccBudget);
        details.duplicateAccepted = r.ok;
        if (!r.ok) {
            details.duplicateResponse = 'HTTP ' + r.status + ' ' + (r.body && r.body.message);
        }

        shapes = [
            { label: 'user field only', body: { budget_scope: 'user', user: user, budget_amount: 5, prevent_further_usage: true } },
            { label: 'user field and entity name', body: { budget_scope: 'user', user: user, budget_entity_name: user, budget_amount: 5, prevent_further_usage: true } },
            { label: 'entity name only', body: { budget_scope: 'user', budget_entity_name: user, budget_amount: 5, prevent_further_usage: true } }
        ];
        details.userBudget = 'all shapes rejected';
        for (i = 0; i < shapes.length; i++) {
            r = await create('create user budget with ' + shapes[i].label, shapes[i].body);
            if (r.ok) {
                details.userBudget = 'accepted with ' + shapes[i].label;
                break;
            }
        }

        r = await create('create enterprise budget', { budget_scope: 'enterprise', budget_amount: 1000, prevent_further_usage: true });
        details.enterpriseBudget = r.ok ? 'accepted' : 'HTTP ' + r.status + ' ' + (r.body && r.body.message);

        r = await ctx.call('list budgets', 'GET', budgetsPath + '?per_page=100');
        if (r.ok) {
            details.listedAfter = (r.body && r.body.budgets ? r.body.budgets : r.body).length;
            details.listHasNextPageFlag = !!r.body && typeof r.body.has_next_page === 'boolean';
        }
    } finally {
        if (keep) {
            ctx.note('SPIKE_KEEP=yes: the cost center and budgets from this run were left in place. Delete them by hand.');
        } else {
            await ctx.cleanup();
        }
    }

    details.manualStep = MANUAL_STEP;
    return ctx.finish('budget-overlap', 'inconclusive',
        'Overlap acceptance recorded (cost center: ' + details.costCenterBudget + '; user: ' + details.userBudget +
        '; enterprise: ' + details.enterpriseBudget + '; exact duplicate accepted: ' + details.duplicateAccepted +
        '). Precedence needs the manual usage step.',
        details);
}

module.exports = { run: run };

if (require.main === module) {
    harness.main(run);
}
