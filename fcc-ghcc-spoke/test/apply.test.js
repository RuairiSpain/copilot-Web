'use strict';

var test = require('node:test');
var assert = require('node:assert/strict');
var lib = require('../lib');
var h = require('./helpers');
var f = require('./plan-fixtures');

var REQ = { requestNumber: 'RITM0100' };

function planAgainstMock(t, desired, policy) {
    return lib.plan(desired, lib.observe(t.client), policy || f.policy(), f.TODAY);
}

test('plan, apply, then plan again leaves nothing to do (idempotent)', function () {
    var t = h.setup({ supportTeams: true });
    var p = planAgainstMock(t, f.desired());
    assert.equal(p.blocked, false, JSON.stringify(p.errors));
    var result = lib.apply(p, t.client, REQ);
    assert.equal(result.ok, true, JSON.stringify(result.results));
    assert.equal(result.results.length, p.operations.length);
    var again = planAgainstMock(t, f.desired());
    assert.deepEqual(again.operations, []);
    assert.equal(again.blocked, false);
});

test('every write is logged with the plan operation it came from', function () {
    var t = h.setup({ supportTeams: true });
    var p = planAgainstMock(t, f.desired());
    lib.apply(p, t.client, REQ);
    assert.equal(t.changeLog.entries.length, p.operations.length);
    assert.equal(p.operations.length, 5);
    t.changeLog.entries.forEach(function (e) {
        assert.equal(e.request, 'RITM0100');
        assert.match(e.operation, /^op-\d+: /);
        assert.equal(e.state, 'done');
    });
});

test('changing a budget and re-applying updates it in place', function () {
    var t = h.setup({ supportTeams: true });
    lib.apply(planAgainstMock(t, f.desired()), t.client, REQ);
    var d = f.desired();
    d.budgets[0].amount = 2500;
    var p = planAgainstMock(t, d);
    assert.deepEqual(f.kinds(p), ['update_budget']);
    assert.equal(lib.apply(p, t.client, REQ).ok, true);
    assert.equal(t.mock.state.budgets[0].budget_amount, 2500);
    assert.equal(t.mock.state.budgets.length, 1);
});

test('an approved move is applied and GitHub does the move', function () {
    var t = h.setup({ supportTeams: true });
    var seed = f.desired();
    seed.costCenters.push({ name: 'Data' });
    seed.users[2].directCostCenter = 'Data';
    lib.apply(planAgainstMock(t, seed), t.client, REQ);
    var d = f.desired();
    d.costCenters.push({ name: 'Data' });
    var blocked = planAgainstMock(t, d);
    assert.deepEqual(f.codes(blocked.errors), ['move_not_approved']);
    assert.throws(function () { lib.apply(blocked, t.client, REQ); }, function (e) { return e.kind === 'precondition'; });
    d.moves = [{ type: 'User', name: 'linus', from: 'Data', to: 'Platform', approvals: { from: true, to: true } }];
    var p = planAgainstMock(t, d);
    assert.equal(lib.apply(p, t.client, REQ).ok, true);
    var platform = t.client.listCostCenters().filter(function (c) { return c.name === 'Platform'; })[0];
    assert.ok(platform.resources.some(function (r) { return r.name === 'linus'; }));
    assert.deepEqual(planAgainstMock(t, d).operations, []);
});

test('a retired cost center and an expired override are removed', function () {
    var t = h.setup({ supportTeams: true });
    var seed = f.desired();
    seed.costCenters.push({ name: 'Old' });
    seed.budgets.push({ scope: 'override', user: 'ada', amount: 100, preventUsage: true, expiry: '2026-11-30', override: true, costCenter: 'Platform', requesterRole: 'team_manager' });
    lib.apply(planAgainstMock(t, seed), t.client, REQ);
    var d = f.desired();
    d.costCenters.push({ name: 'Old', state: 'retired' });
    d.budgets.push({ scope: 'override', user: 'ada', amount: 100, preventUsage: true, expiry: '2026-11-30', override: true, costCenter: 'Platform', requesterRole: 'team_manager' });
    var later = lib.plan(d, lib.observe(t.client), f.policy(), '2026-12-15');
    assert.deepEqual(f.kinds(later), ['delete_budget', 'delete_cost_center']);
    assert.equal(lib.apply(later, t.client, REQ).ok, true);
    assert.equal(t.mock.state.budgets.filter(function (b) { return b.user === 'ada'; }).length, 0);
    assert.equal(t.client.listCostCenters({ state: 'active' }).filter(function (c) { return c.name === 'Old'; }).length, 0);
});

test('apply stops at the first failure and marks the rest skipped, and re-planning resumes', function () {
    var t = h.setup({ supportTeams: true });
    var p = planAgainstMock(t, f.desired());
    t.mock.addFault({ status: 422, method: 'POST', pathIncludes: '/resource', times: 1 });
    var result = lib.apply(p, t.client, REQ);
    assert.equal(result.ok, false);
    var statuses = result.results.map(function (r) { return r.status; });
    assert.deepEqual(statuses, ['done', 'done', 'failed', 'skipped', 'skipped']);
    assert.equal(result.results[2].errorKind, 'validation');
    var resumed = planAgainstMock(t, f.desired());
    assert.deepEqual(f.kinds(resumed), ['add_resources', 'add_resources', 'create_budget']);
    assert.equal(lib.apply(resumed, t.client, REQ).ok, true);
    assert.deepEqual(planAgainstMock(t, f.desired()).operations, []);
});

test('the kill switch stops an apply at its first write', function () {
    var t = h.setup({ supportTeams: true });
    var p = planAgainstMock(t, f.desired());
    t.killed.value = true;
    var result = lib.apply(p, t.client, REQ);
    assert.equal(result.ok, false);
    assert.equal(result.results[0].errorKind, 'kill_switch');
    assert.equal(t.client.listCostCenters().length, 0);
});

test('apply needs a request number', function () {
    var t = h.setup({ supportTeams: true });
    var p = planAgainstMock(t, f.desired());
    assert.throws(function () { lib.apply(p, t.client, {}); }, function (e) { return e.kind === 'precondition'; });
});
