'use strict';

/* A small, valid desired state, plus helpers to vary it in tests. */

function clone(value) {
    return JSON.parse(JSON.stringify(value));
}

function desired() {
    return clone({
        orgs: [{ login: 'acme-platform', fallbackCostCenter: 'UNALLOCATED-acme-platform', state: 'active' }],
        costCenters: [
            { name: 'UNALLOCATED-acme-platform', managed: true },
            { name: 'Platform', managed: true }
        ],
        fundingTeams: [{ slug: 'platform-eng', costCenter: 'Platform' }],
        teamMembers: { 'platform-eng': ['ada', 'grace'] },
        users: [
            { login: 'ada', licensed: true },
            { login: 'grace', licensed: true },
            { login: 'linus', licensed: true, directCostCenter: 'Platform', requestId: 'RITM0001', owner: 'A. Byrne', reviewDate: '2027-01-31' }
        ],
        repos: [{ org: 'acme-platform', repo: 'api', costCenterValue: 'Platform' }],
        budgets: [{ scope: 'cost_center', entity: 'Platform', amount: 1000, preventUsage: true }],
        moves: []
    });
}

/* GitHub exactly as desired() says it should be. */
function observedMatching() {
    return clone({
        costCenters: [
            { id: 'cc-1', name: 'UNALLOCATED-acme-platform', state: 'active', resources: [{ type: 'Org', name: 'acme-platform' }] },
            { id: 'cc-2', name: 'Platform', state: 'active', resources: [{ type: 'Team', name: 'platform-eng' }, { type: 'User', name: 'linus' }, { type: 'Repo', name: 'acme-platform/api' }] }
        ],
        budgets: [{ id: 'b-1', budget_scope: 'cost_center', budget_entity_name: 'Platform', budget_amount: 1000, prevent_further_usage: true }]
    });
}

function observedEmpty() {
    return { costCenters: [], budgets: [] };
}

function policy(overrides) {
    var p = clone({
        teamsSupported: true,
        overrideCapByRole: { team_manager: 200, cc_manager: 500 },
        defaultOverrideCap: 200,
        totalOverrideCap: 2500,
        secondApproverThreshold: 400
    });
    var k;
    for (k in overrides || {}) {
        if (Object.prototype.hasOwnProperty.call(overrides, k)) {
            p[k] = overrides[k];
        }
    }
    return p;
}

var TODAY = '2026-10-31';

function codes(list) {
    return list.map(function (i) { return i.code; });
}

function kinds(plan) {
    return plan.operations.map(function (o) { return o.kind; });
}

module.exports = { clone: clone, desired: desired, observedEmpty: observedEmpty, observedMatching: observedMatching, policy: policy, TODAY: TODAY, codes: codes, kinds: kinds };
