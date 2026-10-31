'use strict';

var util = require('./util');
var budgetmap = require('./budgetmap');
var resolveMod = require('./resolve');
var validateMod = require('./validate');

var lower = util.lower;
var key = util.key;

var KIND_ORDER = ['create_cost_center', 'add_resources', 'move_resources', 'remove_resources',
    'create_budget', 'update_budget', 'delete_budget', 'delete_cost_center'];

/* Policy fields mirror the `policy` table of PRD v0.3. Anything left out is not enforced, except the defaults here. */
function withDefaults(policy) {
    var p = policy || {};
    return {
        maxCostCenters: p.maxCostCenters !== undefined ? p.maxCostCenters : 1000,
        teamsSupported: p.teamsSupported !== undefined ? p.teamsSupported : true,
        allowLicensedUnallocated: p.allowLicensedUnallocated === true,
        overrideCapByRole: p.overrideCapByRole || {},
        defaultOverrideCap: p.defaultOverrideCap,
        totalOverrideCap: p.totalOverrideCap,
        secondApproverThreshold: p.secondApproverThreshold,
        budgetScopeMap: p.budgetScopeMap
    };
}

function listResources(resources, withFrom) {
    var shown = resources.slice(0, 5).map(function (r) {
        return r.type + ' ' + r.name + (withFrom ? ' (from "' + r.from + '")' : '');
    });
    return shown.join(', ') + (resources.length > 5 ? ' and ' + (resources.length - 5) + ' more' : '');
}

function describeBudget(mapped) {
    var target = mapped.user ? 'user ' + mapped.user : (mapped.budget_entity_name ? mapped.budget_scope + ' "' + mapped.budget_entity_name + '"' : mapped.budget_scope);
    return target + ' at ' + util.money(mapped.budget_amount) + ', stop usage ' + (mapped.prevent_further_usage ? 'on' : 'off');
}

function indexCenters(desired, observed) {
    var approved = {};
    var retired = {};
    var observedActive = {};
    var known = {};
    (desired.costCenters || []).forEach(function (c) {
        var s = util.stateOf(c);
        if (s === 'approved') { approved[lower(c.name)] = c; }
        if (s === 'retired') { retired[lower(c.name)] = c; }
    });
    (observed.costCenters || []).forEach(function (c) {
        if (c.state === undefined || c.state === 'active') { observedActive[lower(c.name)] = c; }
    });
    Object.keys(approved).forEach(function (n) { known[n] = true; });
    Object.keys(observedActive).forEach(function (n) { known[n] = true; });
    return { approved: approved, retired: retired, observedActive: observedActive, known: known };
}

function displayName(centers, lowerName) {
    return (centers.approved[lowerName] || centers.observedActive[lowerName] || centers.retired[lowerName]).name;
}

/*
 * Builds a plan: what to create, update, move or delete on GitHub so that the
 * observed state matches the desired state. Pure: no clock, no network.
 *
 *   desired   { orgs, costCenters, fundingTeams, teamMembers, users, repos, budgets, moves }
 *   observed  { costCenters: [{ id, name, state, resources: [{ type, name }] }], budgets: [...] }
 *   policy    see withDefaults
 *   today     'YYYY-MM-DD'; overrides with an earlier expiry are planned for deletion
 *
 * Returns { operations, errors, warnings, blocked, summary }. When blocked is true
 * the operations show what would happen, but apply() refuses to run them.
 */
function plan(desired, observed, policyIn, today) {
    var policy = withDefaults(policyIn);
    var centers = indexCenters(desired, observed);
    var resolution = resolveMod.resolve(desired, policy);
    var check = validateMod.validate(desired, observed, policy, resolution, {
        today: today, known: centers.known, approved: centers.approved, retired: centers.retired, observedActive: centers.observedActive
    });
    var errors = resolution.errors.concat(check.errors);
    var warnings = resolution.warnings.concat(check.warnings);
    var ops = [];
    var location = {};
    var assigned = {};
    var adds = {};
    var moves = {};
    var removes = {};
    var observedBudgets = {};
    var seenBudget = {};

    /* Create approved cost centers that GitHub does not have yet. */
    util.sortBy(Object.keys(centers.approved), function (n) { return n; }).forEach(function (n) {
        if (!centers.observedActive[n]) {
            ops.push({ kind: 'create_cost_center', costCenter: centers.approved[n].name, description: 'Create cost center "' + centers.approved[n].name + '"' });
        }
    });

    /* Where each resource is today. */
    Object.keys(centers.observedActive).forEach(function (n) {
        (centers.observedActive[n].resources || []).forEach(function (r) {
            location[key(r.type, r.name)] = n;
        });
    });

    /* Resources: add what is missing, move what sits in another cost center (needs approval, R5). */
    resolution.assignments.forEach(function (a) {
        var target = lower(a.costCenter);
        var k = key(a.type, a.name);
        var from, approved;
        assigned[k] = true;
        if (!centers.known[target] || centers.retired[target]) {
            return;
        }
        from = location[k];
        if (from === undefined) {
            adds[target] = adds[target] || [];
            adds[target].push({ type: a.type, name: a.name });
        } else if (from !== target) {
            approved = (desired.moves || []).some(function (m) {
                return key(m.type, m.name) === k && lower(m.to) === target && (m.from === undefined || lower(m.from) === from) &&
                    m.approvals && m.approvals.from === true && m.approvals.to === true;
            });
            if (approved) {
                moves[target] = moves[target] || [];
                moves[target].push({ type: a.type, name: a.name, from: centers.observedActive[from].name });
            } else {
                errors.push(util.issue('move_not_approved',
                    a.type + ' ' + a.name + ' is in "' + centers.observedActive[from].name + '" and would move to "' + displayName(centers, target) +
                    '"; a move needs approval from both cost center managers', a.type + ':' + a.name));
            }
        }
    });

    util.sortBy(Object.keys(adds), function (n) { return n; }).forEach(function (n) {
        var list = util.sortBy(adds[n], function (r) { return key(r.type, r.name); });
        ops.push({ kind: 'add_resources', costCenter: displayName(centers, n), resources: list,
            description: 'Add ' + list.length + ' resource' + (list.length === 1 ? '' : 's') + ' to "' + displayName(centers, n) + '": ' + listResources(list) });
    });
    util.sortBy(Object.keys(moves), function (n) { return n; }).forEach(function (n) {
        var list = util.sortBy(moves[n], function (r) { return key(r.type, r.name); });
        ops.push({ kind: 'move_resources', costCenter: displayName(centers, n), resources: list,
            description: 'Move ' + list.length + ' resource' + (list.length === 1 ? '' : 's') + ' into "' + displayName(centers, n) + '": ' + listResources(list, true) });
    });

    /* Remove resources from managed cost centers when nothing desires them anywhere. */
    util.sortBy(Object.keys(centers.approved), function (n) { return n; }).forEach(function (n) {
        var obs = centers.observedActive[n];
        var extra;
        if (!obs || centers.approved[n].managed === false) {
            return;
        }
        extra = (obs.resources || []).filter(function (r) { return !assigned[key(r.type, r.name)]; })
            .map(function (r) { return { type: r.type, name: r.name }; });
        if (extra.length) {
            extra = util.sortBy(extra, function (r) { return key(r.type, r.name); });
            removes[n] = extra;
        }
    });
    util.sortBy(Object.keys(removes), function (n) { return n; }).forEach(function (n) {
        ops.push({ kind: 'remove_resources', costCenter: displayName(centers, n), resources: removes[n],
            description: 'Remove ' + removes[n].length + ' resource' + (removes[n].length === 1 ? '' : 's') + ' from "' + displayName(centers, n) + '": ' + listResources(removes[n]) });
    });

    /* Budgets. */
    (observed.budgets || []).forEach(function (b) {
        observedBudgets[budgetmap.identityOfObserved(b)] = b;
    });
    (desired.budgets || []).forEach(function (b) {
        var s = util.stateOf(b);
        var mapped, id, obs, expired, entityKnown;
        if (s === 'draft') {
            return;
        }
        mapped = budgetmap.toGithub(b, policy.budgetScopeMap);
        if (!mapped.budget_scope) {
            return;
        }
        id = budgetmap.identityOfMapped(mapped);
        if (seenBudget[id]) {
            return;
        }
        seenBudget[id] = true;
        obs = observedBudgets[id];
        expired = b.scope === 'override' && b.expiry && b.expiry < today;
        if (s === 'retired' || expired) {
            if (obs) {
                ops.push({ kind: 'delete_budget', githubId: obs.id, reason: expired ? 'expired' : 'retired', budget: mapped,
                    description: 'Delete ' + (expired ? 'expired ' : '') + 'budget: ' + describeBudget(mapped) });
            }
            return;
        }
        entityKnown = !budgetmap.COST_CENTER_SCOPES[b.scope] || centers.known[lower(b.entity)];
        if (!entityKnown) {
            return;
        }
        if (!obs) {
            ops.push({ kind: 'create_budget', budget: mapped, description: 'Create budget: ' + describeBudget(mapped) });
        } else if (obs.budget_amount !== mapped.budget_amount || obs.prevent_further_usage !== mapped.prevent_further_usage) {
            ops.push({ kind: 'update_budget', githubId: obs.id, budget: mapped,
                changes: { budget_amount: mapped.budget_amount, prevent_further_usage: mapped.prevent_further_usage },
                description: 'Change budget ' + (mapped.user ? 'for user ' + mapped.user : mapped.budget_scope + (mapped.budget_entity_name ? ' "' + mapped.budget_entity_name + '"' : '')) +
                    ' from ' + util.money(obs.budget_amount) + ' (stop usage ' + (obs.prevent_further_usage ? 'on' : 'off') + ') to ' + describeBudget(mapped).replace(/^.* at /, '') });
        }
    });

    /* Retire cost centers last, after their resources and budgets are handled. */
    util.sortBy(Object.keys(centers.retired), function (n) { return n; }).forEach(function (n) {
        if (centers.observedActive[n]) {
            ops.push({ kind: 'delete_cost_center', costCenter: centers.retired[n].name, githubId: centers.observedActive[n].id,
                description: 'Delete cost center "' + centers.retired[n].name + '"' });
        }
    });

    ops = ops.map(function (op, i) { return { op: i, kind: op.kind, rank: KIND_ORDER.indexOf(op.kind), body: op }; });
    ops = util.sortBy(ops, function (o) { return (100 + o.rank) + '-' + (1000000 + o.op); }).map(function (o, i) {
        var out = {};
        var k;
        out.id = 'op-' + (i + 1);
        for (k in o.body) {
            if (Object.prototype.hasOwnProperty.call(o.body, k)) { out[k] = o.body[k]; }
        }
        return out;
    });

    return {
        operations: ops,
        errors: errors,
        warnings: warnings,
        blocked: errors.length > 0,
        summary: KIND_ORDER.reduce(function (acc, kind) {
            acc[kind] = ops.filter(function (o) { return o.kind === kind; }).length;
            return acc;
        }, {})
    };
}

module.exports = { plan: plan, withDefaults: withDefaults, indexCenters: indexCenters, KIND_ORDER: KIND_ORDER };
