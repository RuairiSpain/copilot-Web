'use strict';

var errors = require('./errors');
var util = require('./util');

var TYPE_KEY = { User: 'users', Org: 'organizations', Repo: 'repositories', Team: 'teams' };

function toResourceBody(resources) {
    var body = {};
    resources.forEach(function (r) {
        var k = TYPE_KEY[r.type];
        if (!k) {
            throw new errors.GHCCError(errors.KINDS.VALIDATION, 'Unknown resource type "' + r.type + '"');
        }
        body[k] = body[k] || [];
        body[k].push(r.name);
    });
    return body;
}

/*
 * Runs a plan's operations in order through GHCCClient, one change-log entry per
 * GitHub write. Refuses a blocked plan. Stops at the first failure and marks the
 * rest skipped, so a half-applied plan is visible and re-planning picks up from
 * where it stopped.
 *
 *   plan   the result of plan()
 *   client a GHCCClient
 *   meta   { requestNumber, replan, expectedFingerprint, allowStale }
 *            requestNumber        shared by every operation in the plan
 *            replan               function () -> plan, built from a fresh observe(); required unless allowStale.
 *                                 apply() refuses to run when the fresh plan's operations differ from the plan
 *                                 that was approved, because GitHub changed between approval and apply.
 *            expectedFingerprint  optional; the fingerprint recorded at approval. It is compared with a fingerprint
 *                                 recomputed from plan.operations, so a plan edited since approval is refused.
 *            allowStale           true skips the freshness check (tests, or remediation plans built a moment ago)
 *
 * Returns { ok, results: [{ id, kind, status, error? }] }; status is done, failed or skipped.
 */
function apply(plan, client, meta) {
    var ids = {};
    var results = [];
    var failed = false;
    var i, op, res, body, fresh, actual;

    if (!plan || !Array.isArray(plan.operations)) {
        throw new errors.GHCCError(errors.KINDS.PRECONDITION, 'apply needs a plan with an operations list');
    }
    /* Trust nothing stored on the plan: blocked and the fingerprint are recomputed from its contents. */
    if (plan.blocked || (plan.errors && plan.errors.length > 0)) {
        throw new errors.GHCCError(errors.KINDS.PRECONDITION, 'the plan is blocked by ' + (plan.errors ? plan.errors.length : 0) + ' validation error(s); fix them before applying');
    }
    if (!meta || !meta.requestNumber) {
        throw new errors.GHCCError(errors.KINDS.PRECONDITION, 'a request or change number is required to apply a plan');
    }

    actual = util.fingerprint(plan.operations);
    if (plan.fingerprint !== undefined && plan.fingerprint !== actual) {
        throw new errors.GHCCError(errors.KINDS.PRECONDITION, 'the plan operations do not match the plan fingerprint; the plan was edited, so plan again');
    }
    if (meta.expectedFingerprint && meta.expectedFingerprint !== actual) {
        throw new errors.GHCCError(errors.KINDS.PRECONDITION, 'the plan changed after it was approved (fingerprint mismatch); review and approve it again');
    }
    if (meta.allowStale !== true) {
        if (typeof meta.replan !== 'function') {
            throw new errors.GHCCError(errors.KINDS.PRECONDITION, 'apply needs meta.replan to confirm GitHub still matches the approved plan (or meta.allowStale for a plan built just now)');
        }
        fresh = meta.replan();
        if (fresh.blocked || util.fingerprint(fresh.operations) !== actual) {
            throw new errors.GHCCError(errors.KINDS.PRECONDITION, 'GitHub or ServiceNow changed since this plan was approved; re-plan and approve again');
        }
    }

    client.listCostCenters({ state: 'active' }).forEach(function (c) {
        ids[util.lower(c.name)] = c.id;
    });

    function idOf(name) {
        var id = ids[util.lower(name)];
        if (!id) {
            throw new errors.GHCCError(errors.KINDS.NOT_FOUND, 'cost center "' + name + '" is not in GitHub');
        }
        return id;
    }

    function stepMeta(o) {
        return { requestNumber: meta.requestNumber, operation: o.id + ': ' + o.description };
    }

    for (i = 0; i < plan.operations.length; i++) {
        op = plan.operations[i];
        if (failed) {
            results.push({ id: op.id, kind: op.kind, status: 'skipped' });
            continue;
        }
        try {
            if (op.kind === 'create_cost_center') {
                res = client.createCostCenter({ name: op.costCenter }, stepMeta(op));
                ids[util.lower(op.costCenter)] = res.costCenter.id;
            } else if (op.kind === 'add_resources' || op.kind === 'move_resources') {
                client.addResources(idOf(op.costCenter), toResourceBody(op.resources), stepMeta(op));
            } else if (op.kind === 'remove_resources') {
                client.removeResources(idOf(op.costCenter), toResourceBody(op.resources), stepMeta(op));
            } else if (op.kind === 'create_budget') {
                client.createBudget(op.budget, stepMeta(op));
            } else if (op.kind === 'update_budget') {
                client.updateBudget(op.githubId, op.changes, stepMeta(op));
            } else if (op.kind === 'delete_budget') {
                client.deleteBudget(op.githubId, stepMeta(op));
            } else if (op.kind === 'delete_cost_center') {
                client.deleteCostCenter(op.githubId, stepMeta(op));
            } else {
                throw new errors.GHCCError(errors.KINDS.UNKNOWN, 'unknown operation kind "' + op.kind + '"');
            }
            results.push({ id: op.id, kind: op.kind, status: 'done' });
        } catch (e) {
            failed = true;
            body = { id: op.id, kind: op.kind, status: 'failed', error: e.message, errorKind: e.kind };
            results.push(body);
        }
    }
    return { ok: !failed, results: results };
}

/* Reads the observed state the planner needs from GitHub. */
function observe(client) {
    return {
        costCenters: client.listCostCenters({ state: 'active' }),
        budgets: client.listBudgets()
    };
}

module.exports = { apply: apply, observe: observe };
