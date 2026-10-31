'use strict';

var util = require('./util');
var budgetmap = require('./budgetmap');
var resolveMod = require('./resolve');
var planMod = require('./plan');
var reconcileMod = require('./reconcile');

var lower = util.lower;
var key = util.key;

var TYPES = {
    BUDGET_CHANGED: 'budget_changed',
    MEMBERSHIP_CHANGED: 'membership_changed',
    RESOURCE_MOVED: 'resource_moved',
    UNMANAGED_OBJECT: 'unmanaged_object',
    MISSING_OBJECT: 'missing_object',
    REPO_PROPERTY_DRIFT: 'repo_property_drift',
    ORG_BASELINE_MISSING: 'org_baseline_missing'
};

var MODE_RANK = { report: 0, approve: 1, auto: 2 };

/*
 * The most automatic mode each drift type may run in. A configured mode above
 * this is lowered, because the PRD requires a manager (membership) or both
 * managers (moves, R5) to approve, and because nothing safe can be undone
 * automatically for the other types.
 */
var MAX_MODE = {
    budget_changed: 'auto',
    missing_object: 'auto',
    membership_changed: 'approve',
    resource_moved: 'approve',
    unmanaged_object: 'report',
    repo_property_drift: 'report',
    org_baseline_missing: 'report'
};

/* What PRD 4.5 suggests once an org has run in report mode long enough. Every org starts in report mode (X1). */
var RECOMMENDED_MODES = {
    budget_changed: 'auto',
    missing_object: 'auto',
    membership_changed: 'approve',
    resource_moved: 'approve',
    unmanaged_object: 'report',
    repo_property_drift: 'report',
    org_baseline_missing: 'report'
};

var TYPE_ORDER = [TYPES.ORG_BASELINE_MISSING, TYPES.MISSING_OBJECT, TYPES.BUDGET_CHANGED, TYPES.MEMBERSHIP_CHANGED,
    TYPES.RESOURCE_MOVED, TYPES.REPO_PROPERTY_DRIFT, TYPES.UNMANAGED_OBJECT];

function budgetText(amount, prevent) {
    return util.money(amount) + ', stop usage ' + (prevent ? 'on' : 'off');
}

/*
 * Compares what GitHub holds with what ServiceNow says it should hold, and
 * reports every difference as a drift item.
 *
 * `desired` must hold only records that have already been applied. A change
 * that is approved but not yet applied is pending, not drift; plan() handles it.
 *
 *   observed.costCenters   as for plan()
 *   observed.budgets       as for plan()
 *   observed.repoProperties  optional [{ org, repo, value }]
 *   observed.orgProperties   optional { orgLogin: { costCenterDefined } }
 *
 *   options.policy   as for plan() (teamsSupported, budgetScopeMap)
 *   options.modes    { defaults: { type: mode }, orgs: { orgLogin: { type: mode } } };
 *                    mode is report, approve or auto, and defaults to report for everything
 *
 * Each item: { key, type, subject, org, desired, observed, configuredMode, mode, modeNote,
 *   remediation, moveRequest, accept, actions, autoRemediate }.
 * `remediation` is a plan-shaped object that apply() can run; it is null when nothing
 * safe can be done automatically. `key` is stable: the same difference always gets the same key.
 */
function detectDrift(desired, observed, options) {
    var opts = options || {};
    var today = util.requireToday(opts.today, 'detectDrift');
    var policy = planMod.withDefaults(opts.policy);
    var modes = opts.modes || {};
    var centers = planMod.indexCenters(desired, observed);
    var resolution = resolveMod.resolve(desired, policy);
    var items = [];
    var location = centers.location;
    var assigned = util.dict();
    var missingCenters = util.dict();
    var desiredNames = util.dict();
    var desiredBudgetIds = util.dict();
    var observedBudgetById = util.dict();

    (observed.budgets || []).forEach(function (o) { observedBudgetById[budgetmap.identityOfObserved(o)] = o; });
    (desired.costCenters || []).forEach(function (c) {
        desiredNames[lower(c.name)] = true;
    });

    function modeFor(type, org) {
        var configured = (org && util.own(util.own(modes.orgs, org), type)) || util.own(modes.defaults, type) || 'report';
        var max = MAX_MODE[type];
        var effective = MODE_RANK[configured] > MODE_RANK[max] ? max : configured;
        return { configured: configured, effective: effective, note: effective !== configured ? 'lowered from ' + configured + ': ' + type + ' can run at most in ' + max + ' mode' : null };
    }

    function remediation(ops, requiresApproval) {
        var numbered = ops.map(function (o, i) {
            var out = { id: 'op-' + (i + 1) };
            var k;
            for (k in o) { if (Object.prototype.hasOwnProperty.call(o, k)) { out[k] = o[k]; } }
            return out;
        });
        return {
            operations: numbered,
            fingerprint: util.fingerprint(numbered),
            errors: [],
            warnings: [],
            blocked: false,
            requiresApproval: requiresApproval === true
        };
    }

    function add(type, subject, desiredText, observedText, extra) {
        var x = extra || {};
        var m = modeFor(type, x.org);
        var rem = x.remediation || null;
        var actions = [];
        if (rem) { actions.push('remediate'); }
        if (x.moveRequest) { actions.push('request_move'); }
        if (x.adopt) { actions.push('adopt'); }
        if (x.accept) { actions.push('accept'); }
        items.push({
            key: type + ':' + subject + ':' + util.hash(desiredText + '|' + observedText),
            type: type,
            subject: subject,
            org: x.org || null,
            desired: desiredText,
            observed: observedText,
            configuredMode: m.configured,
            mode: m.effective,
            modeNote: m.note,
            remediation: rem,
            moveRequest: x.moveRequest || null,
            accept: x.accept || null,
            actions: actions,
            autoRemediate: m.effective === 'auto' && rem !== null && rem.requiresApproval !== true
        });
    }

    resolution.assignments.forEach(function (a) { assigned[key(a.type, a.name)] = true; });

    /* Org baseline: the UNALLOCATED fallback and the cost_center property. */
    (desired.orgs || []).forEach(function (org) {
        var fallback = org.fallbackCostCenter || ('UNALLOCATED-' + org.login);
        var props = observed.orgProperties && observed.orgProperties[org.login];
        if (org.state !== undefined && org.state !== 'active') { return; }
        if (!centers.observedActive[lower(fallback)]) {
            missingCenters[lower(fallback)] = true;
            add(TYPES.ORG_BASELINE_MISSING, 'Org:' + org.login, 'fallback cost center "' + fallback + '"', 'not found',
                { org: org.login, remediation: remediation([{ kind: 'create_cost_center', costCenter: fallback, description: 'Create cost center "' + fallback + '"' }], false) });
        }
        if (props && props.costCenterDefined === false) {
            add(TYPES.ORG_BASELINE_MISSING, 'Org:' + org.login + '/property', 'custom property cost_center defined', 'not defined', { org: org.login });
        }
    });

    /* Cost centers that should exist. */
    util.sortBy(Object.keys(centers.approved), function (n) { return n; }).forEach(function (n) {
        var name = centers.approved[n].name;
        if (!centers.observedActive[n] && !missingCenters[n]) {
            missingCenters[n] = true;
            add(TYPES.MISSING_OBJECT, 'CostCenter:' + name, 'exists', 'not found in GitHub',
                { remediation: remediation([{ kind: 'create_cost_center', costCenter: name, description: 'Create cost center "' + name + '"' }], false) });
        }
    });

    /* Budgets: missing, changed, or not ours. */
    (desired.budgets || []).forEach(function (b) {
        var s = util.stateOf(b);
        var mapped, id, obs, subject;
        if (s !== 'approved' || (b.scope === 'override' && b.expiry && b.expiry < today)) {
            if (s !== 'draft') { desiredBudgetIds[budgetmap.identityOfMapped(budgetmap.toGithub(b, policy.budgetScopeMap))] = true; }
            return;
        }
        mapped = budgetmap.toGithub(b, policy.budgetScopeMap);
        id = budgetmap.identityOfMapped(mapped);
        desiredBudgetIds[id] = true;
        subject = 'Budget:' + b.scope + ':' + (b.entity || b.user || 'all');
        obs = observedBudgetById[id];
        if (!obs) {
            add(TYPES.MISSING_OBJECT, subject, budgetText(mapped.budget_amount, mapped.prevent_further_usage), 'not found in GitHub',
                { remediation: remediation([{ kind: 'create_budget', budget: mapped, description: 'Create budget: ' + budgetText(mapped.budget_amount, mapped.prevent_further_usage) }], false) });
        } else if (obs.budget_amount !== mapped.budget_amount || obs.prevent_further_usage !== mapped.prevent_further_usage) {
            add(TYPES.BUDGET_CHANGED, subject, budgetText(mapped.budget_amount, mapped.prevent_further_usage), budgetText(obs.budget_amount, obs.prevent_further_usage), {
                remediation: remediation([{ kind: 'update_budget', githubId: obs.id, budget: mapped,
                    changes: { budget_amount: mapped.budget_amount, prevent_further_usage: mapped.prevent_further_usage },
                    description: 'Restore budget ' + subject + ' to ' + budgetText(mapped.budget_amount, mapped.prevent_further_usage) }], false),
                accept: { description: 'Accept ' + budgetText(obs.budget_amount, obs.prevent_further_usage) + ' as the desired budget for ' + subject,
                    set: { budget: subject, amount: obs.budget_amount, preventUsage: obs.prevent_further_usage } }
            });
        }
    });
    (observed.budgets || []).forEach(function (o) {
        var id = budgetmap.identityOfObserved(o);
        if (!desiredBudgetIds[id]) {
            add(TYPES.UNMANAGED_OBJECT, 'Budget:' + o.budget_scope + ':' + (o.budget_entity_name || o.user || 'all'), 'not in ServiceNow', budgetText(o.budget_amount, o.prevent_further_usage),
                { adopt: { description: 'Adopt this budget as an unmanaged record', githubId: o.id }, accept: null });
        }
    });

    /* Resources: removed, moved or added outside the spoke. */
    resolution.assignments.forEach(function (a) {
        var target = lower(a.costCenter);
        var k = key(a.type, a.name);
        var from = location[k];
        var targetName = centers.approved[target] ? centers.approved[target].name : a.costCenter;
        if (!centers.observedActive[target]) { return; }
        if (from === undefined) {
            add(TYPES.MEMBERSHIP_CHANGED, a.type + ':' + a.name, 'in "' + targetName + '"', 'in no cost center', {
                remediation: remediation([{ kind: 'add_resources', costCenter: targetName, resources: [{ type: a.type, name: a.name }],
                    description: 'Add ' + a.type + ' ' + a.name + ' back to "' + targetName + '"' }], true),
                accept: { description: 'Accept that ' + a.type + ' ' + a.name + ' is no longer in "' + targetName + '"', set: { assignment: k, remove: true } }
            });
        } else if (from !== target) {
            add(TYPES.RESOURCE_MOVED, a.type + ':' + a.name, 'in "' + targetName + '"', 'in "' + centers.observedActive[from].name + '"', {
                moveRequest: { type: a.type, name: a.name, from: centers.observedActive[from].name, to: targetName },
                accept: { description: 'Accept "' + centers.observedActive[from].name + '" as the cost center of ' + a.type + ' ' + a.name, set: { assignment: k, costCenter: centers.observedActive[from].name } }
            });
        }
    });
    util.sortBy(Object.keys(centers.approved), function (n) { return n; }).forEach(function (n) {
        var obs = centers.observedActive[n];
        var name = centers.approved[n].name;
        if (!obs || centers.approved[n].managed !== true) { return; }
        (obs.resources || []).forEach(function (r) {
            if (!assigned[key(r.type, r.name)]) {
                add(TYPES.MEMBERSHIP_CHANGED, r.type + ':' + r.name, 'not in "' + name + '"', 'in "' + name + '"', {
                    remediation: remediation([{ kind: 'remove_resources', costCenter: name, resources: [{ type: r.type, name: r.name }],
                        description: 'Remove ' + r.type + ' ' + r.name + ' from "' + name + '"' }], true),
                    accept: { description: 'Accept ' + r.type + ' ' + r.name + ' as a member of "' + name + '"', set: { assignment: key(r.type, r.name), costCenter: name } }
                });
            }
        });
    });

    /* Cost centers GitHub has that ServiceNow does not know. */
    Object.keys(centers.observedActive).forEach(function (n) {
        if (!desiredNames[n]) {
            add(TYPES.UNMANAGED_OBJECT, 'CostCenter:' + centers.observedActive[n].name, 'not in ServiceNow',
                'exists with ' + (centers.observedActive[n].resources || []).length + ' resources',
                { adopt: { description: 'Adopt this cost center as an unmanaged record', githubId: centers.observedActive[n].id } });
        }
    });

    /*
     * Repo properties: only repos ServiceNow has a record for. A repo whose property is missing
     * or matches no cost center, with no ServiceNow record, is an anomaly (repo_property), not drift,
     * so the same condition is not raised twice.
     */
    if (observed.repoProperties) {
        (function () {
            var observedByRepo = util.dict();
            observed.repoProperties.forEach(function (o) { observedByRepo[lower(o.org) + '/' + lower(o.repo)] = o; });
            (desired.repos || []).forEach(function (r) {
                var o = observedByRepo[lower(r.org) + '/' + lower(r.repo)];
                var observedValue = o ? (o.value || '') : '';
                if (lower(observedValue) !== lower(r.costCenterValue)) {
                    add(TYPES.REPO_PROPERTY_DRIFT, 'Repo:' + r.org + '/' + r.repo, 'cost_center "' + (r.costCenterValue || '') + '"',
                        observedValue ? 'cost_center "' + observedValue + '"' : 'no cost_center property',
                        { org: r.org, accept: { description: observedValue ? 'Accept "' + observedValue + '" as the property value' : 'Accept that the repo has no cost_center property',
                            set: { repo: r.org + '/' + r.repo, costCenterValue: observedValue } } });
                }
            });
        }());
    }

    items = util.sortBy(items, function (i) { return (100 + TYPE_ORDER.indexOf(i.type)) + '|' + i.subject + '|' + i.key; });
    return items;
}

/*
 * Reconciles freshly detected drift with the drift records ServiceNow already
 * holds, so a repeated sync never creates duplicates (X4).
 *
 *   existing  [{ key, state }]; state is open, awaiting_approval, dismissed, accepted, remediated or resolved
 *
 * Returns { create, resolve, unchanged }; see reconcile.js. Dismissed drift is not raised again while it persists; once it disappears the dismissed record resolves, so a recurrence is raised afresh.
 */
function reconcile(detected, existing) {
    return reconcileMod.reconcileRecords(detected, existing, ['open', 'awaiting_approval', 'dismissed'], ['open', 'awaiting_approval', 'dismissed']);
}

module.exports = {
    detectDrift: detectDrift,
    reconcile: reconcile,
    TYPES: TYPES,
    MAX_MODE: MAX_MODE,
    RECOMMENDED_MODES: RECOMMENDED_MODES
};
