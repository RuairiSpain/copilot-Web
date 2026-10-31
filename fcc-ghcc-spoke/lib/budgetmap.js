'use strict';

var util = require('./util');

/*
 * Maps the five budget scopes of the PRD (the `budget.scope` field in ServiceNow)
 * to the fields GitHub's budget API takes.
 *
 * ASSUMED, confirm after the Phase 0 budget-overlap spike: which GitHub scope
 * stands for "every licensed user" and "each user in one cost center". The map
 * can be replaced through policy.budgetScopeMap without touching the planner.
 */
var DEFAULT_SCOPE_MAP = {
    enterprise: 'enterprise',
    universal_user: 'multi_user',
    cost_center: 'cost_center',
    cost_center_user: 'multi_user',
    override: 'user'
};

/* Scopes whose entity is a cost center name. */
var COST_CENTER_SCOPES = { cost_center: true, cost_center_user: true };

function toGithub(budget, scopeMap) {
    var map = scopeMap || DEFAULT_SCOPE_MAP;
    var scope = map[budget.scope];
    var out = {
        budget_scope: scope,
        budget_entity_name: '',
        budget_amount: budget.amount,
        prevent_further_usage: budget.preventUsage
    };
    if (COST_CENTER_SCOPES[budget.scope]) {
        out.budget_entity_name = budget.entity || '';
    }
    if (budget.scope === 'override') {
        out.user = budget.user;
    }
    return out;
}

/* What makes two budgets "the same budget": scope, entity and user. The entity is ignored for user budgets. */
function identity(scope, entity, user) {
    return scope + '|' + (scope === 'user' ? '' : util.lower(entity)) + '|' + util.lower(user);
}

function identityOfMapped(mapped) {
    return identity(mapped.budget_scope, mapped.budget_entity_name, mapped.user);
}

function identityOfObserved(observed) {
    return identity(observed.budget_scope, observed.budget_entity_name, observed.user);
}

module.exports = {
    DEFAULT_SCOPE_MAP: DEFAULT_SCOPE_MAP,
    COST_CENTER_SCOPES: COST_CENTER_SCOPES,
    toGithub: toGithub,
    identityOfMapped: identityOfMapped,
    identityOfObserved: identityOfObserved
};
