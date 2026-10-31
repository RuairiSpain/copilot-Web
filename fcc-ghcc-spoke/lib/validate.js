'use strict';

var util = require('./util');
var budgetmap = require('./budgetmap');

var issue = util.issue;
var lower = util.lower;

/*
 * Validation rules from PRD section 6, as pure functions.
 *
 * context: { today, known, approved, retired, observedActive }
 *   known           lower-case names of cost centers that exist or will exist in this apply set
 *   approved        approved desired cost centers by lower-case name
 *   retired         retired desired cost centers by lower-case name
 *   observedActive  observed active cost centers by lower-case name
 *
 * Returns { errors, warnings }. Any error blocks the plan.
 */
function validate(desired, observed, policy, resolution, context) {
    var errors = [];
    var warnings = [];
    var today = context.today;
    var i, u, b, subject, seen, mapped, id, cap, role, total, byCenter, count;

    /* Duplicate cost center names. */
    seen = util.dict();
    (desired.costCenters || []).forEach(function (center) {
        var n = lower(center.name);
        if (util.stateOf(center) === 'draft') {
            return;
        }
        if (seen[n]) {
            errors.push(issue('duplicate_cost_center', 'Cost center "' + center.name + '" is defined more than once', 'CostCenter:' + center.name));
        }
        seen[n] = true;
    });

    /* R1: every active org has exactly one UNALLOCATED fallback cost center. */
    (desired.orgs || []).forEach(function (org) {
        var fallback;
        if (org.state !== undefined && org.state !== 'active') {
            return;
        }
        fallback = lower(org.fallbackCostCenter || ('UNALLOCATED-' + org.login));
        if (!context.known[fallback]) {
            errors.push(issue('org_without_fallback',
                'Org ' + org.login + ' has no UNALLOCATED fallback cost center (expected "' + (org.fallbackCostCenter || ('UNALLOCATED-' + org.login)) + '")', 'Org:' + org.login));
        }
    });

    /* R3: a direct assignment needs a request ID, an owner and a review date. */
    for (i = 0; i < (desired.users || []).length; i++) {
        u = desired.users[i];
        if (!u.directCostCenter) {
            continue;
        }
        if (!u.requestId || !u.owner || !u.reviewDate) {
            errors.push(issue('direct_assignment_incomplete',
                'Direct assignment of ' + u.login + ' needs a request ID, an owner and a review date', 'User:' + u.login));
        } else if (u.reviewDate < today) {
            warnings.push(issue('direct_assignment_review_overdue',
                'Direct assignment of ' + u.login + ' was due for review on ' + u.reviewDate, 'User:' + u.login));
        }
    }

    /* A licensed user must resolve to a real, non-fallback cost center. */
    (desired.users || []).forEach(function (user) {
        var center, isFallback, problem;
        if (!user.licensed) {
            return;
        }
        center = resolution.userCenter[lower(user.login)];
        isFallback = center && /^unallocated-/.test(lower(center));
        if (!center || isFallback) {
            problem = issue('licensed_user_unallocated',
                'Licensed user ' + user.login + ' ' + (center ? 'would sit in the fallback cost center ' + center : 'resolves to no cost center'), 'User:' + user.login);
            (policy.allowLicensedUnallocated ? warnings : errors).push(problem);
        }
    });

    /* Every assignment must point at a cost center that exists or is created in this apply set. */
    seen = util.dict();
    resolution.assignments.forEach(function (as) {
        var target = lower(as.costCenter);
        if (context.retired[target]) {
            errors.push(issue('retired_cost_center_in_use',
                as.type + ' ' + as.name + ' is still assigned to retired cost center "' + as.costCenter + '"', as.type + ':' + as.name));
        } else if (!context.known[target] && !seen[target + as.type + as.name]) {
            seen[target + as.type + as.name] = true;
            errors.push(issue('assignment_to_unknown_cost_center',
                as.type + ' ' + as.name + ' is assigned to "' + as.costCenter + '", which is not an approved cost center', as.type + ':' + as.name));
        }
    });

    /* At most the GitHub limit of active cost centers. */
    count = 0;
    seen = util.dict();
    Object.keys(context.known).forEach(function (n) {
        if (!context.retired[n] && !seen[n]) {
            seen[n] = true;
            count++;
        }
    });
    if (count > policy.maxCostCenters) {
        errors.push(issue('too_many_cost_centers',
            'The apply set would leave ' + count + ' active cost centers; the limit is ' + policy.maxCostCenters, null));
    }

    /* Budgets. */
    seen = util.dict();
    byCenter = util.dict();
    for (i = 0; i < (desired.budgets || []).length; i++) {
        b = desired.budgets[i];
        if (util.stateOf(b) === 'draft' || util.stateOf(b) === 'retired') {
            continue;
        }
        subject = 'Budget:' + b.scope + ':' + (b.entity || b.user || 'all');
        mapped = budgetmap.toGithub(b, policy.budgetScopeMap);

        if (!mapped.budget_scope) {
            errors.push(issue('budget_scope_unmapped', 'Budget scope "' + b.scope + '" has no GitHub scope in the scope map', subject));
            continue;
        }
        if (typeof b.amount !== 'number' || b.amount < 0 || isNaN(b.amount)) {
            errors.push(issue('budget_amount_invalid', 'Budget amount must be a number of zero or more', subject));
            continue;
        }
        if ((b.amount === 0 || b.preventUsage !== true) && b.exception !== true) {
            errors.push(issue('budget_not_protective',
                'Budget ' + (b.amount === 0 ? 'is $0' : 'has stop usage off') + ' without a policy exception', subject));
        }
        if (b.scope === 'override' && !b.user) {
            errors.push(issue('override_without_user', 'An individual override must name a user', subject));
        }
        if (util.own(budgetmap.COST_CENTER_SCOPES, b.scope) && !context.known[lower(b.entity)]) {
            errors.push(issue('budget_on_unknown_cost_center',
                'Budget is on cost center "' + b.entity + '", which does not exist in GitHub and is not in this apply set', subject));
        }
        id = budgetmap.identityOfMapped(mapped);
        if (seen[id]) {
            errors.push(issue('duplicate_budget', 'More than one desired budget targets the same scope and entity', subject));
        }
        seen[id] = true;

        if (b.scope === 'override' && !(b.expiry && b.expiry < today)) {
            if (!b.expiry) {
                errors.push(issue('override_without_expiry', 'An individual override needs an expiry date', subject));
            }
            role = b.requesterRole;
            cap = util.own(policy.overrideCapByRole, role) !== undefined ? util.own(policy.overrideCapByRole, role) : policy.defaultOverrideCap;
            if (cap !== undefined && b.amount > cap) {
                errors.push(issue('override_above_cap',
                    'Override of ' + util.money(b.amount) + ' is above the ' + util.money(cap) + ' cap for ' + (role || 'this role'), subject));
            }
            if (policy.secondApproverThreshold !== undefined && b.amount > policy.secondApproverThreshold && !b.secondApprover) {
                errors.push(issue('second_approver_required',
                    'Override of ' + util.money(b.amount) + ' is above ' + util.money(policy.secondApproverThreshold) + ' and needs a second approver', subject));
            }
            if (b.costCenter) {
                byCenter[lower(b.costCenter)] = (byCenter[lower(b.costCenter)] || 0) + b.amount;
            }
        }
    }
    if (policy.totalOverrideCap !== undefined) {
        Object.keys(byCenter).forEach(function (n) {
            total = byCenter[n];
            if (total > policy.totalOverrideCap) {
                errors.push(issue('total_overrides_above_cap',
                    'Overrides in cost center "' + n + '" total ' + util.money(total) + ', above the ' + util.money(policy.totalOverrideCap) + ' cap', 'CostCenter:' + n));
            }
        });
    }

    return { errors: errors, warnings: warnings };
}

module.exports = { validate: validate };
