'use strict';

var test = require('node:test');
var assert = require('node:assert/strict');
var lib = require('../lib');
var f = require('./plan-fixtures');

function run(desired, observed, policy) {
    return lib.plan(desired, observed || f.observedEmpty(), policy || f.policy(), f.TODAY);
}

function op(plan, kind, center) {
    return plan.operations.filter(function (o) { return o.kind === kind && (!center || o.costCenter === center); })[0];
}

test('a valid desired state against an empty GitHub creates everything, in a safe order', function () {
    var p = run(f.desired());
    assert.equal(p.blocked, false, JSON.stringify(p.errors));
    assert.deepEqual(f.kinds(p), ['create_cost_center', 'create_cost_center', 'add_resources', 'add_resources', 'create_budget']);
    assert.deepEqual(p.operations.map(function (o) { return o.id; }), ['op-1', 'op-2', 'op-3', 'op-4', 'op-5']);
    assert.deepEqual(op(p, 'add_resources', 'UNALLOCATED-acme-platform').resources, [{ type: 'Org', name: 'acme-platform' }]);
    assert.deepEqual(op(p, 'add_resources', 'Platform').resources, [
        { type: 'Repo', name: 'acme-platform/api' },
        { type: 'Team', name: 'platform-eng' },
        { type: 'User', name: 'linus' }
    ]);
    assert.deepEqual(p.summary, { create_cost_center: 2, add_resources: 2, move_resources: 0, remove_resources: 0, create_budget: 1, update_budget: 0, delete_budget: 0, delete_cost_center: 0 });
});

test('every operation carries a plain-language description for the plan screen', function () {
    var p = run(f.desired());
    p.operations.forEach(function (o) { assert.ok(o.description && o.description.length > 10, o.kind); });
    assert.equal(p.operations[0].description, 'Create cost center "Platform"');
});

test('when GitHub cannot take teams, funding teams expand to their members and a direct assignment beats the team', function () {
    var d = f.desired();
    d.users[0].directCostCenter = 'UNALLOCATED-acme-platform';
    d.users[0].requestId = 'RITM9'; d.users[0].owner = 'x'; d.users[0].reviewDate = '2027-02-01';
    var p = run(d, f.observedEmpty(), f.policy({ teamsSupported: false }));
    var platform = op(p, 'add_resources', 'Platform').resources.map(function (r) { return r.type + ':' + r.name; });
    assert.deepEqual(platform, ['Repo:acme-platform/api', 'User:grace', 'User:linus']);
    assert.ok(f.codes(p.warnings).indexOf('direct_assignment_overrides_team') !== -1);
});

test('planning is deterministic', function () {
    assert.equal(JSON.stringify(run(f.desired())), JSON.stringify(run(f.desired())));
});

test('nothing to do when GitHub already matches', function () {
    var observed = {
        costCenters: [
            { id: 'cc-1', name: 'UNALLOCATED-acme-platform', state: 'active', resources: [{ type: 'Org', name: 'acme-platform' }] },
            { id: 'cc-2', name: 'Platform', state: 'active', resources: [{ type: 'Team', name: 'platform-eng' }, { type: 'User', name: 'linus' }, { type: 'Repo', name: 'acme-platform/api' }] }
        ],
        budgets: [{ id: 'b-1', budget_scope: 'cost_center', budget_entity_name: 'Platform', budget_amount: 1000, prevent_further_usage: true }]
    };
    var p = run(f.desired(), observed);
    assert.deepEqual(p.operations, []);
    assert.equal(p.blocked, false);
});

test('resource names are compared case-insensitively', function () {
    var observed = {
        costCenters: [
            { id: 'cc-1', name: 'unallocated-ACME-platform', state: 'active', resources: [{ type: 'Org', name: 'ACME-Platform' }] },
            { id: 'cc-2', name: 'platform', state: 'active', resources: [{ type: 'Team', name: 'Platform-Eng' }, { type: 'User', name: 'Linus' }, { type: 'Repo', name: 'Acme-Platform/API' }] }
        ],
        budgets: [{ id: 'b-1', budget_scope: 'cost_center', budget_entity_name: 'PLATFORM', budget_amount: 1000, prevent_further_usage: true }]
    };
    assert.deepEqual(run(f.desired(), observed).operations, []);
});

test('a changed budget becomes an update that names the old and new amount', function () {
    var d = f.desired();
    d.budgets[0].amount = 1500;
    var observed = {
        costCenters: [
            { id: 'cc-1', name: 'UNALLOCATED-acme-platform', state: 'active', resources: [{ type: 'Org', name: 'acme-platform' }] },
            { id: 'cc-2', name: 'Platform', state: 'active', resources: [{ type: 'Team', name: 'platform-eng' }, { type: 'User', name: 'linus' }, { type: 'Repo', name: 'acme-platform/api' }] }
        ],
        budgets: [{ id: 'b-1', budget_scope: 'cost_center', budget_entity_name: 'Platform', budget_amount: 1000, prevent_further_usage: true }]
    };
    var p = run(d, observed);
    assert.deepEqual(f.kinds(p), ['update_budget']);
    assert.equal(p.operations[0].id.length > 0, true);
    assert.deepEqual(p.operations[0].changes, { budget_amount: 1500, prevent_further_usage: true });
    assert.match(p.operations[0].description, /from \$1000 \(stop usage on\) to \$1500, stop usage on/);
});

test('a resource in another cost center is not moved without both managers approving (R5)', function () {
    var observed = {
        costCenters: [
            { id: 'cc-1', name: 'UNALLOCATED-acme-platform', state: 'active', resources: [{ type: 'Org', name: 'acme-platform' }] },
            { id: 'cc-3', name: 'Data', state: 'active', resources: [{ type: 'User', name: 'linus' }] }
        ],
        budgets: []
    };
    var p = run(f.desired(), observed);
    assert.equal(p.blocked, true);
    assert.deepEqual(f.codes(p.errors), ['move_not_approved']);
    assert.match(p.errors[0].message, /both cost center managers/);
    var added = op(p, 'add_resources', 'Platform').resources.map(function (r) { return r.name; });
    assert.equal(added.indexOf('linus'), -1, 'the unapproved move must not be planned');
});

test('an approved move is planned, naming where it comes from', function () {
    var observed = {
        costCenters: [
            { id: 'cc-1', name: 'UNALLOCATED-acme-platform', state: 'active', resources: [{ type: 'Org', name: 'acme-platform' }] },
            { id: 'cc-3', name: 'Data', state: 'active', resources: [{ type: 'User', name: 'linus' }] }
        ],
        budgets: []
    };
    var d = f.desired();
    d.moves = [{ type: 'User', name: 'linus', from: 'Data', to: 'Platform', approvals: { from: true, to: true } }];
    var p = run(d, observed);
    assert.equal(p.blocked, false, JSON.stringify(p.errors));
    assert.deepEqual(op(p, 'move_resources').resources, [{ type: 'User', name: 'linus', from: 'Data' }]);
    assert.equal(op(p, 'remove_resources'), undefined, 'GitHub moves the resource itself');
});

test('a half-approved or stale move is still blocked', function () {
    var observed = {
        costCenters: [
            { id: 'cc-1', name: 'UNALLOCATED-acme-platform', state: 'active', resources: [{ type: 'Org', name: 'acme-platform' }] },
            { id: 'cc-3', name: 'Data', state: 'active', resources: [{ type: 'User', name: 'linus' }] }
        ],
        budgets: []
    };
    var half = f.desired();
    half.moves = [{ type: 'User', name: 'linus', from: 'Data', to: 'Platform', approvals: { from: true, to: false } }];
    assert.deepEqual(f.codes(run(half, observed).errors), ['move_not_approved']);
    var stale = f.desired();
    stale.moves = [{ type: 'User', name: 'linus', from: 'Elsewhere', to: 'Platform', approvals: { from: true, to: true } }];
    assert.deepEqual(f.codes(run(stale, observed).errors), ['move_not_approved']);
});

test('resources nobody wants are removed from a managed cost center, but not from an unmanaged one', function () {
    var d = f.desired();
    var observed = {
        costCenters: [
            { id: 'cc-1', name: 'UNALLOCATED-acme-platform', state: 'active', resources: [{ type: 'Org', name: 'acme-platform' }] },
            { id: 'cc-2', name: 'Platform', state: 'active', resources: [{ type: 'Team', name: 'platform-eng' }, { type: 'User', name: 'linus' }, { type: 'Repo', name: 'acme-platform/api' }, { type: 'User', name: 'mallory' }] },
            { id: 'cc-9', name: 'Pilot', state: 'active', resources: [{ type: 'User', name: 'someone' }] }
        ],
        budgets: [{ id: 'b-1', budget_scope: 'cost_center', budget_entity_name: 'Platform', budget_amount: 1000, prevent_further_usage: true }]
    };
    var p = run(d, observed);
    assert.deepEqual(f.kinds(p), ['remove_resources']);
    assert.deepEqual(p.operations[0].resources, [{ type: 'User', name: 'mallory' }]);
    d.costCenters[1].managed = false;
    assert.deepEqual(run(d, observed).operations, []);
});

test('overrides: a live one is created, an expired one is deleted, and no expiry blocks the plan', function () {
    var d = f.desired();
    d.budgets = [
        { scope: 'override', user: 'ada', amount: 150, preventUsage: true, expiry: '2026-11-30', override: true, costCenter: 'Platform', requesterRole: 'team_manager' },
        { scope: 'override', user: 'grace', amount: 100, preventUsage: true, expiry: '2026-10-28', override: true, costCenter: 'Platform', requesterRole: 'team_manager' }
    ];
    var observed = {
        costCenters: [
            { id: 'cc-1', name: 'UNALLOCATED-acme-platform', state: 'active', resources: [{ type: 'Org', name: 'acme-platform' }] },
            { id: 'cc-2', name: 'Platform', state: 'active', resources: [{ type: 'Team', name: 'platform-eng' }, { type: 'User', name: 'linus' }, { type: 'Repo', name: 'acme-platform/api' }] }
        ],
        budgets: [{ id: 'b-7', budget_scope: 'user', budget_entity_name: '', user: 'grace', budget_amount: 100, prevent_further_usage: true }]
    };
    var p = run(d, observed);
    assert.equal(p.blocked, false, JSON.stringify(p.errors));
    assert.deepEqual(f.kinds(p), ['create_budget', 'delete_budget']);
    assert.equal(p.operations[1].id, 'op-2');
    assert.equal(p.operations[1].reason, 'expired');
    d.budgets[0].expiry = undefined;
    assert.ok(f.codes(run(d, observed).errors).indexOf('override_without_expiry') !== -1);
});

test('a retired cost center is deleted last, and only once nothing is assigned to it', function () {
    var d = f.desired();
    d.costCenters.push({ name: 'Old', managed: true, state: 'retired' });
    var observed = {
        costCenters: [
            { id: 'cc-1', name: 'UNALLOCATED-acme-platform', state: 'active', resources: [{ type: 'Org', name: 'acme-platform' }] },
            { id: 'cc-2', name: 'Platform', state: 'active', resources: [{ type: 'Team', name: 'platform-eng' }, { type: 'User', name: 'linus' }, { type: 'Repo', name: 'acme-platform/api' }] },
            { id: 'cc-8', name: 'Old', state: 'active', resources: [] }
        ],
        budgets: [{ id: 'b-1', budget_scope: 'cost_center', budget_entity_name: 'Platform', budget_amount: 1000, prevent_further_usage: true }]
    };
    var p = run(d, observed);
    assert.deepEqual(f.kinds(p), ['delete_cost_center']);
    assert.equal(p.operations[0].id, 'op-1');
    d.users[2].directCostCenter = 'Old';
    assert.ok(f.codes(run(d, observed).errors).indexOf('retired_cost_center_in_use') !== -1);
});

test('a budget scope with no mapping blocks the plan instead of guessing', function () {
    var d = f.desired();
    d.budgets[0].scope = 'something_new';
    assert.ok(f.codes(run(d).errors).indexOf('budget_scope_unmapped') !== -1);
});

test('the scope map can be replaced from policy', function () {
    var d = f.desired();
    d.budgets.push({ scope: 'universal_user', amount: 50, preventUsage: true });
    var p = run(d, f.observedEmpty(), f.policy({ budgetScopeMap: { enterprise: 'enterprise', universal_user: 'user_default', cost_center: 'cost_center', cost_center_user: 'multi_user', override: 'user' } }));
    var created = p.operations.filter(function (o) { return o.kind === 'create_budget'; }).map(function (o) { return o.budget.budget_scope; });
    assert.deepEqual(created.sort(), ['cost_center', 'user_default']);
});

test('plan, drift and anomalies all refuse a missing or malformed today', function () {
    [undefined, '', '2026-1-5', 20261031].forEach(function (bad) {
        assert.throws(function () { lib.plan(f.desired(), f.observedEmpty(), f.policy(), bad); }, /today is required/);
    });
    assert.throws(function () { lib.detectDrift(f.desired(), f.observedEmpty(), { policy: f.policy() }); }, /today is required/);
    assert.throws(function () { lib.detectAnomalies(f.desired(), f.observedEmpty(), {}, {}); }, /today is required/);
});

test('an expired override is deleted only because today is given', function () {
    var d = f.desired();
    d.budgets = [{ scope: 'override', user: 'grace', amount: 100, preventUsage: true, expiry: '2020-01-01', override: true, costCenter: 'Platform', requesterRole: 'team_manager' }];
    var observed = f.observedEmpty();
    observed.budgets = [{ id: 'b-9', budget_scope: 'user', budget_entity_name: '', user: 'grace', budget_amount: 100, prevent_further_usage: true }];
    assert.ok(f.kinds(run(d, observed)).indexOf('delete_budget') !== -1);
});

test('a cost center is not managed unless it says so: extras are left alone by default', function () {
    var d = f.desired();
    delete d.costCenters[1].managed;
    var observed = removalObserved(['mallory']);
    assert.deepEqual(run(d, observed).operations, []);
    d.costCenters[1].managed = true;
    assert.deepEqual(f.kinds(run(d, observed)), ['remove_resources']);
});

function removalObserved(extraLogins) {
    var resources = [{ type: 'Team', name: 'platform-eng' }, { type: 'User', name: 'linus' }, { type: 'Repo', name: 'acme-platform/api' }];
    extraLogins.forEach(function (n) { resources.push({ type: 'User', name: n }); });
    return {
        costCenters: [
            { id: 'cc-1', name: 'UNALLOCATED-acme-platform', state: 'active', resources: [{ type: 'Org', name: 'acme-platform' }] },
            { id: 'cc-2', name: 'Platform', state: 'active', resources: resources }
        ],
        budgets: [{ id: 'b-1', budget_scope: 'cost_center', budget_entity_name: 'Platform', budget_amount: 1000, prevent_further_usage: true }]
    };
}

test('removing more than policy.maxRemovals blocks the plan', function () {
    var logins = [];
    var i;
    for (i = 0; i < 6; i++) { logins.push('extra' + i); }
    var policy = f.policy();
    policy.maxRemovals = 5;
    var p = run(f.desired(), removalObserved(logins), policy);
    assert.deepEqual(f.codes(p.errors), ['too_many_removals']);
    assert.equal(p.blocked, true);
    policy.maxRemovals = 6;
    assert.equal(run(f.desired(), removalObserved(logins), policy).blocked, false);
});

test('a plan that removes resources while nothing is desired anywhere is blocked as an incomplete desired state', function () {
    var d = f.desired();
    d.orgs = [];
    d.fundingTeams = [];
    d.users = [];
    d.repos = [];
    d.budgets = [];
    var p = run(d, removalObserved(['mallory']));
    assert.ok(f.codes(p.errors).indexOf('desired_state_empty') !== -1);
    assert.equal(p.blocked, true);
});

test('a plan has a fingerprint that changes when its operations change', function () {
    var a = run(f.desired(), f.observedEmpty());
    var b = run(f.desired(), f.observedEmpty());
    assert.equal(a.fingerprint, b.fingerprint);
    var d = f.desired();
    d.budgets[0].amount = 1;
    assert.notEqual(run(d, f.observedEmpty()).fingerprint, a.fingerprint);
});
