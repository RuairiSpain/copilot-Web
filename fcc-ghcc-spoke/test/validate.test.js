'use strict';

var test = require('node:test');
var assert = require('node:assert/strict');
var lib = require('../lib');
var f = require('./plan-fixtures');

function errorsOf(desired, policy, observed) {
    return f.codes(lib.plan(desired, observed || f.observedEmpty(), policy || f.policy(), f.TODAY).errors);
}

function warningsOf(desired, policy) {
    return f.codes(lib.plan(desired, f.observedEmpty(), policy || f.policy(), f.TODAY).warnings);
}

test('the baseline fixture has no errors or warnings', function () {
    var p = lib.plan(f.desired(), f.observedEmpty(), f.policy(), f.TODAY);
    assert.deepEqual(p.errors, []);
    assert.deepEqual(p.warnings, []);
});

test('R2: a user in funding teams of two cost centers blocks the plan', function () {
    var d = f.desired();
    d.costCenters.push({ name: 'Data' });
    d.fundingTeams.push({ slug: 'data-sci', costCenter: 'Data' });
    d.teamMembers['data-sci'] = ['ada'];
    var codes = errorsOf(d);
    assert.ok(codes.indexOf('user_in_teams_of_different_cost_centers') !== -1);
    /* The conflicted user resolves to nothing, so the licensed-user rule also fires. */
    assert.ok(codes.indexOf('licensed_user_unallocated') !== -1);
});

test('R2: two teams in the same cost center is only a warning', function () {
    var d = f.desired();
    d.fundingTeams.push({ slug: 'platform-sre', costCenter: 'Platform' });
    d.teamMembers['platform-sre'] = ['ada'];
    assert.deepEqual(errorsOf(d), []);
    assert.deepEqual(warningsOf(d), ['user_in_multiple_teams_same_center']);
});

test('R1: an org without an UNALLOCATED cost center blocks the plan', function () {
    var d = f.desired();
    d.costCenters = d.costCenters.filter(function (c) { return c.name !== 'UNALLOCATED-acme-platform'; });
    assert.ok(errorsOf(d).indexOf('org_without_fallback') !== -1);
});

test('R1: the fallback is satisfied by one that already exists in GitHub', function () {
    var d = f.desired();
    d.costCenters = d.costCenters.filter(function (c) { return c.name !== 'UNALLOCATED-acme-platform'; });
    var observed = { costCenters: [{ id: 'cc-1', name: 'UNALLOCATED-acme-platform', state: 'active', resources: [] }], budgets: [] };
    assert.equal(errorsOf(d, null, observed).indexOf('org_without_fallback'), -1);
});

test('R3: a direct assignment missing a request ID, owner or review date blocks the plan', function () {
    ['requestId', 'owner', 'reviewDate'].forEach(function (field) {
        var d = f.desired();
        delete d.users[2][field];
        assert.deepEqual(errorsOf(d), ['direct_assignment_incomplete'], field);
    });
});

test('R3: an overdue review date warns but does not block', function () {
    var d = f.desired();
    d.users[2].reviewDate = '2026-09-01';
    assert.deepEqual(errorsOf(d), []);
    assert.deepEqual(warningsOf(d), ['direct_assignment_review_overdue']);
});

test('a licensed user with no cost center, or only the fallback, blocks the plan', function () {
    var d = f.desired();
    d.users.push({ login: 'nobody', licensed: true });
    assert.deepEqual(errorsOf(d), ['licensed_user_unallocated']);
    var e = f.desired();
    e.users.push({ login: 'parked', licensed: true, directCostCenter: 'UNALLOCATED-acme-platform', requestId: 'R', owner: 'o', reviewDate: '2027-01-01' });
    assert.deepEqual(errorsOf(e), ['licensed_user_unallocated']);
});

test('an unlicensed user may sit in no cost center, and the rule can be relaxed to a warning during rollout', function () {
    var d = f.desired();
    d.users.push({ login: 'contractor', licensed: false });
    assert.deepEqual(errorsOf(d), []);
    d.users.push({ login: 'nobody', licensed: true });
    assert.deepEqual(errorsOf(d, f.policy({ allowLicensedUnallocated: true })), []);
    assert.ok(warningsOf(d, f.policy({ allowLicensedUnallocated: true })).indexOf('licensed_user_unallocated') !== -1);
});

test('an assignment to a Draft or unknown cost center blocks the plan', function () {
    var d = f.desired();
    d.costCenters[1].state = 'draft';
    var codes = errorsOf(d);
    assert.ok(codes.indexOf('assignment_to_unknown_cost_center') !== -1);
});

test('more than the maximum number of active cost centers blocks the plan', function () {
    assert.ok(errorsOf(f.desired(), f.policy({ maxCostCenters: 1 })).indexOf('too_many_cost_centers') !== -1);
    assert.equal(errorsOf(f.desired(), f.policy({ maxCostCenters: 2 })).indexOf('too_many_cost_centers'), -1);
});

test('the limit counts cost centers already in GitHub and ignores retired ones', function () {
    var observed = { costCenters: [{ id: 'cc-9', name: 'Pilot', state: 'active', resources: [] }], budgets: [] };
    assert.ok(errorsOf(f.desired(), f.policy({ maxCostCenters: 2 }), observed).indexOf('too_many_cost_centers') !== -1);
    var d = f.desired();
    d.costCenters.push({ name: 'Pilot', state: 'retired' });
    assert.equal(errorsOf(d, f.policy({ maxCostCenters: 2 }), observed).indexOf('too_many_cost_centers'), -1);
});

test('a budget on a cost center that is neither in GitHub nor in the apply set blocks the plan', function () {
    var d = f.desired();
    d.budgets.push({ scope: 'cost_center', entity: 'Nowhere', amount: 100, preventUsage: true });
    assert.deepEqual(errorsOf(d), ['budget_on_unknown_cost_center']);
});

test('a budget on a Draft cost center blocks, but one created in the same apply set does not', function () {
    var d = f.desired();
    d.costCenters[1].state = 'draft';
    assert.ok(errorsOf(d).indexOf('budget_on_unknown_cost_center') !== -1);
    assert.equal(errorsOf(f.desired()).indexOf('budget_on_unknown_cost_center'), -1);
});

test('a $0 budget or one with stop usage off blocks unless an exception is recorded', function () {
    var zero = f.desired();
    zero.budgets[0].amount = 0;
    assert.deepEqual(errorsOf(zero), ['budget_not_protective']);
    var open = f.desired();
    open.budgets[0].preventUsage = false;
    assert.deepEqual(errorsOf(open), ['budget_not_protective']);
    open.budgets[0].exception = true;
    assert.deepEqual(errorsOf(open), []);
});

test('an invalid amount is reported once, not as several follow-on errors', function () {
    var d = f.desired();
    d.budgets[0].amount = 'lots';
    assert.deepEqual(errorsOf(d), ['budget_amount_invalid']);
});

test('B5: an override above the role cap blocks, and a missing role falls back to the default cap', function () {
    var d = f.desired();
    d.budgets = [{ scope: 'override', user: 'ada', amount: 300, preventUsage: true, expiry: '2026-11-30', override: true, costCenter: 'Platform', requesterRole: 'team_manager' }];
    assert.deepEqual(errorsOf(d), ['override_above_cap']);
    d.budgets[0].requesterRole = 'cc_manager';
    assert.deepEqual(errorsOf(d), []);
    d.budgets[0].requesterRole = undefined;
    assert.deepEqual(errorsOf(d), ['override_above_cap']);
});

test('B5: above the second-approver threshold needs a second approver', function () {
    var d = f.desired();
    d.budgets = [{ scope: 'override', user: 'ada', amount: 450, preventUsage: true, expiry: '2026-11-30', override: true, costCenter: 'Platform', requesterRole: 'cc_manager' }];
    assert.ok(errorsOf(d).indexOf('second_approver_required') !== -1);
    d.budgets[0].secondApprover = 'M. Kowalski';
    assert.equal(errorsOf(d).indexOf('second_approver_required'), -1);
});

test('B5: overrides in one cost center are capped in total, counting only live ones', function () {
    var d = f.desired();
    d.budgets = [];
    ['ada', 'grace', 'linus'].forEach(function (u) {
        d.budgets.push({ scope: 'override', user: u, amount: 400, preventUsage: true, expiry: '2026-11-30', override: true, costCenter: 'Platform', requesterRole: 'cc_manager' });
    });
    assert.equal(errorsOf(d, f.policy({ totalOverrideCap: 1000 })).filter(function (c) { return c === 'total_overrides_above_cap'; }).length, 1);
    d.budgets[2].expiry = '2026-10-01';
    assert.equal(errorsOf(d, f.policy({ totalOverrideCap: 1000 })).indexOf('total_overrides_above_cap'), -1);
});

test('two desired budgets for the same target block the plan', function () {
    var d = f.desired();
    d.budgets.push({ scope: 'cost_center', entity: 'platform', amount: 2000, preventUsage: true });
    assert.deepEqual(errorsOf(d), ['duplicate_budget']);
});

test('a cost center defined twice blocks the plan', function () {
    var d = f.desired();
    d.costCenters.push({ name: 'platform' });
    assert.ok(errorsOf(d).indexOf('duplicate_cost_center') !== -1);
});

test('repos with no or an invalid property warn and are left on the org fallback', function () {
    var d = f.desired();
    d.repos.push({ org: 'acme-platform', repo: 'legacy', costCenterValue: '' });
    d.repos.push({ org: 'acme-platform', repo: 'typo', costCenterValue: 'Platfrom' });
    assert.deepEqual(errorsOf(d), []);
    assert.deepEqual(warningsOf(d), ['repo_without_property', 'repo_invalid_property']);
    var p = lib.plan(d, f.observedEmpty(), f.policy(), f.TODAY);
    var added = p.operations.filter(function (o) { return o.kind === 'add_resources'; });
    assert.equal(JSON.stringify(added).indexOf('legacy'), -1);
    assert.equal(JSON.stringify(added).indexOf('typo'), -1);
});
