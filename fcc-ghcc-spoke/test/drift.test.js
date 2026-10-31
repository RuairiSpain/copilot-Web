'use strict';

var test = require('node:test');
var assert = require('node:assert/strict');
var lib = require('../lib');
var h = require('./helpers');
var f = require('./plan-fixtures');

function detect(observed, options, desired) {
    var opts = { policy: f.policy(), today: f.TODAY };
    var k;
    for (k in options || {}) { if (Object.prototype.hasOwnProperty.call(options, k)) { opts[k] = options[k]; } }
    return lib.detectDrift(desired || f.desired(), observed, opts);
}

function types(items) {
    return items.map(function (i) { return i.type; });
}

function find(items, type) {
    return items.filter(function (i) { return i.type === type; })[0];
}

test('no drift when GitHub matches what ServiceNow applied', function () {
    assert.deepEqual(detect(f.observedMatching()), []);
});

test('an edited budget is drift, with a revert and an accept option', function () {
    var o = f.observedMatching();
    o.budgets[0].budget_amount = 5000;
    var items = detect(o);
    assert.deepEqual(types(items), ['budget_changed']);
    var d = items[0];
    assert.equal(d.desired, '$1000, stop usage on');
    assert.equal(d.observed, '$5000, stop usage on');
    assert.deepEqual(d.actions, ['remediate', 'accept']);
    assert.equal(d.remediation.operations[0].kind, 'update_budget');
    assert.equal(d.remediation.operations[0].githubId, 'b-1');
    assert.deepEqual(d.accept.set, { budget: 'Budget:cost_center:Platform', amount: 5000, preventUsage: true });
});

test('stop usage switched off is drift too', function () {
    var o = f.observedMatching();
    o.budgets[0].prevent_further_usage = false;
    assert.equal(find(detect(o), 'budget_changed').observed, '$1000, stop usage off');
});

test('every org starts in report mode, so nothing remediates by itself (X1)', function () {
    var o = f.observedMatching();
    o.budgets[0].budget_amount = 5000;
    var d = detect(o)[0];
    assert.equal(d.mode, 'report');
    assert.equal(d.autoRemediate, false);
});

test('a budget change auto-remediates once its type is set to auto', function () {
    var o = f.observedMatching();
    o.budgets[0].budget_amount = 5000;
    var d = detect(o, { modes: { defaults: { budget_changed: 'auto' } } })[0];
    assert.equal(d.mode, 'auto');
    assert.equal(d.autoRemediate, true);
});

test('membership and move drift can never run above approve mode, and say why', function () {
    var o = f.observedMatching();
    o.costCenters[1].resources = o.costCenters[1].resources.filter(function (r) { return r.name !== 'linus'; });
    var d = detect(o, { modes: { defaults: { membership_changed: 'auto', resource_moved: 'auto' } } })[0];
    assert.equal(d.type, 'membership_changed');
    assert.equal(d.configuredMode, 'auto');
    assert.equal(d.mode, 'approve');
    assert.equal(d.autoRemediate, false);
    assert.match(d.modeNote, /lowered from auto/);
});

test('a user removed from a cost center outside the spoke is drift; the fix adds them back after approval', function () {
    var o = f.observedMatching();
    o.costCenters[1].resources = o.costCenters[1].resources.filter(function (r) { return r.name !== 'linus'; });
    var d = detect(o)[0];
    assert.equal(d.subject, 'User:linus');
    assert.equal(d.remediation.requiresApproval, true);
    assert.deepEqual(d.remediation.operations[0].resources, [{ type: 'User', name: 'linus' }]);
});

test('a resource added outside the spoke to a managed cost center is drift; the fix removes it', function () {
    var o = f.observedMatching();
    o.costCenters[1].resources.push({ type: 'User', name: 'mallory' });
    var d = detect(o)[0];
    assert.equal(d.type, 'membership_changed');
    assert.equal(d.subject, 'User:mallory');
    assert.equal(d.remediation.operations[0].kind, 'remove_resources');
});

test('extras in an unmanaged cost center are left alone', function () {
    var o = f.observedMatching();
    o.costCenters[1].resources.push({ type: 'User', name: 'mallory' });
    var d = f.desired();
    d.costCenters[1].managed = false;
    assert.deepEqual(detect(o, {}, d), []);
});

test('a resource in another cost center is a move: alert, with a move request, never an automatic fix (R5)', function () {
    var o = f.observedMatching();
    o.costCenters[1].resources = o.costCenters[1].resources.filter(function (r) { return r.name !== 'linus'; });
    o.costCenters.push({ id: 'cc-3', name: 'Data', state: 'active', resources: [{ type: 'User', name: 'linus' }] });
    var d = detect(o, { modes: { defaults: { resource_moved: 'auto' } } });
    var moved = find(d, 'resource_moved');
    assert.ok(moved);
    assert.deepEqual(moved.moveRequest, { type: 'User', name: 'linus', from: 'Data', to: 'Platform' });
    assert.equal(moved.remediation, null);
    assert.equal(moved.autoRemediate, false);
    assert.deepEqual(moved.actions, ['request_move', 'accept']);
    assert.equal(find(d, 'unmanaged_object').subject, 'CostCenter:Data');
});

test('a cost center deleted in GitHub is missing, and the drift that follows from it is not reported separately', function () {
    var o = f.observedMatching();
    o.costCenters = o.costCenters.filter(function (c) { return c.name !== 'Platform'; });
    o.budgets = [];
    var d = detect(o);
    /* The team, user and repo that sat in Platform are not listed: they come back when the cost center does. */
    assert.deepEqual(d.map(function (i) { return i.subject; }), ['Budget:cost_center:Platform', 'CostCenter:Platform']);
    assert.deepEqual(types(d), ['missing_object', 'missing_object']);
    assert.equal(types(d).indexOf('membership_changed'), -1);
    assert.equal(d[0].remediation.operations[0].kind, 'create_budget');
    assert.equal(d[1].remediation.operations[0].kind, 'create_cost_center');
});

test('a budget deleted in GitHub is missing and is recreated', function () {
    var o = f.observedMatching();
    o.budgets = [];
    var d = detect(o);
    assert.deepEqual(types(d), ['missing_object']);
    assert.equal(d[0].remediation.operations[0].kind, 'create_budget');
});

test('cost centers and budgets GitHub has but ServiceNow does not are unmanaged: adopt or accept, never removed', function () {
    var o = f.observedMatching();
    o.costCenters.push({ id: 'cc-9', name: 'Pilot', state: 'active', resources: [{ type: 'User', name: 'someone' }] });
    o.budgets.push({ id: 'b-9', budget_scope: 'enterprise', budget_entity_name: '', budget_amount: 9000, prevent_further_usage: true });
    var d = detect(o);
    assert.deepEqual(types(d), ['unmanaged_object', 'unmanaged_object']);
    d.forEach(function (i) {
        assert.deepEqual(i.actions, ['adopt']);
        assert.equal(i.remediation, null);
        assert.equal(i.mode, 'report');
    });
    var capped = detect(o, { modes: { defaults: { unmanaged_object: 'auto' } } });
    assert.equal(capped[0].mode, 'report');
});

test('a retired cost center that still exists is not reported as unmanaged', function () {
    var o = f.observedMatching();
    o.costCenters.push({ id: 'cc-8', name: 'Old', state: 'active', resources: [] });
    var d = f.desired();
    d.costCenters.push({ name: 'Old', state: 'retired' });
    assert.deepEqual(detect(o, {}, d), []);
});

test('a missing UNALLOCATED fallback is an org baseline problem, reported once', function () {
    var o = f.observedMatching();
    o.costCenters = o.costCenters.filter(function (c) { return c.name !== 'UNALLOCATED-acme-platform'; });
    var d = detect(o);
    assert.deepEqual(d.map(function (i) { return i.type + ' ' + i.subject; }), ['org_baseline_missing Org:acme-platform']);
    assert.equal(d[0].org, 'acme-platform');
    assert.equal(d[0].remediation.operations[0].costCenter, 'UNALLOCATED-acme-platform');
});

test('an org without the cost_center property defined is a baseline problem with no automatic fix', function () {
    var o = f.observedMatching();
    o.orgProperties = { 'acme-platform': { costCenterDefined: false } };
    var d = detect(o);
    assert.equal(d[0].type, 'org_baseline_missing');
    assert.equal(d[0].subject, 'Org:acme-platform/property');
    assert.equal(d[0].remediation, null);
});

test('repo property drift: a known repo whose property was edited or removed; unknown repos are anomalies, not drift', function () {
    var o = f.observedMatching();
    o.repoProperties = [
        { org: 'acme-platform', repo: 'api', value: 'Data' },
        { org: 'acme-platform', repo: 'new-thing', value: 'Platfrom' },
        { org: 'acme-platform', repo: 'bare', value: '' }
    ];
    var d = detect(o);
    assert.deepEqual(types(d), ['repo_property_drift']);
    assert.equal(d[0].subject, 'Repo:acme-platform/api');
    assert.equal(d[0].observed, 'cost_center "Data"');
    assert.equal(d[0].org, 'acme-platform');
    o.repoProperties = [];
    var gone = detect(o);
    assert.equal(gone.length, 1);
    assert.equal(gone[0].observed, 'no cost_center property');
});

test('repo properties are not checked when GitHub properties were not read', function () {
    assert.deepEqual(detect(f.observedMatching()), []);
});

test('modes can be set per org for org-scoped drift, and fall back to the defaults', function () {
    var o = f.observedMatching();
    o.repoProperties = [{ org: 'acme-platform', repo: 'api', value: 'Data' }];
    var d = detect(o, { modes: { defaults: { repo_property_drift: 'approve' }, orgs: { 'acme-platform': { repo_property_drift: 'report' } } } });
    assert.equal(d[0].configuredMode, 'report');
    var e = detect(o, { modes: { defaults: { repo_property_drift: 'approve' } } });
    assert.equal(e[0].configuredMode, 'approve');
    assert.equal(e[0].mode, 'report', 'capped: nothing to remediate automatically');
});

test('detection is deterministic and each drift has a stable key', function () {
    var o = f.observedMatching();
    o.budgets[0].budget_amount = 5000;
    o.costCenters[1].resources.push({ type: 'User', name: 'mallory' });
    var a = detect(o);
    var b = detect(o);
    assert.equal(JSON.stringify(a), JSON.stringify(b));
    assert.equal(new Set(a.map(function (i) { return i.key; })).size, a.length);
    o.budgets[0].budget_amount = 6000;
    assert.notEqual(find(detect(o), 'budget_changed').key, find(a, 'budget_changed').key, 'a different observed value is a different drift');
});

test('reconcile: a repeated sync creates nothing new and resolves what has gone (X4)', function () {
    var o = f.observedMatching();
    o.budgets[0].budget_amount = 5000;
    o.costCenters[1].resources.push({ type: 'User', name: 'mallory' });
    var first = lib.reconcileDrift(detect(o), []);
    assert.equal(first.create.length, 2);
    var existing = first.create.map(function (d) { return { key: d.key, state: 'open' }; });
    var second = lib.reconcileDrift(detect(o), existing);
    assert.equal(second.create.length, 0);
    assert.equal(second.unchanged.length, 2);
    assert.deepEqual(second.resolve, []);
    o.budgets[0].budget_amount = 1000;
    var third = lib.reconcileDrift(detect(o), existing);
    assert.equal(third.resolve.length, 1);
    assert.equal(third.create.length, 0);
});

test('reconcile: a dismissed drift is not raised again, but an accepted or resolved one can recur', function () {
    var o = f.observedMatching();
    o.budgets[0].budget_amount = 5000;
    var d = detect(o);
    assert.equal(lib.reconcileDrift(d, [{ key: d[0].key, state: 'dismissed' }]).create.length, 0);
    assert.equal(lib.reconcileDrift(d, [{ key: d[0].key, state: 'resolved' }]).create.length, 1);
});

test('reconcile: a dismissed drift resolves when the difference goes, so a recurrence is raised again', function () {
    var o = f.observedMatching();
    o.budgets[0].budget_amount = 5000;
    var d = detect(o);
    var dismissed = [{ key: d[0].key, state: 'dismissed' }];
    assert.deepEqual(lib.reconcileDrift(d, dismissed).resolve, []);
    assert.deepEqual(lib.reconcileDrift([], dismissed).resolve, [d[0].key]);
    assert.equal(lib.reconcileDrift(d, [{ key: d[0].key, state: 'resolved' }]).create.length, 1);
});

test('remediating budget drift through apply() against the mock restores GitHub, and drift disappears', function () {
    var t = h.setup({ supportTeams: true });
    var REQ = { requestNumber: 'RITM0200' };
    assert.equal(lib.apply(lib.plan(f.desired(), lib.observe(t.client), f.policy(), f.TODAY), t.client, { requestNumber: 'RITM0100', allowStale: true }).ok, true);
    assert.deepEqual(lib.detectDrift(f.desired(), lib.observe(t.client), { policy: f.policy(), today: f.TODAY }), []);

    t.client.updateBudget(t.mock.state.budgets[0].id, { budget_amount: 7777 }, { requestNumber: 'GITHUB-UI' });
    var drift = lib.detectDrift(f.desired(), lib.observe(t.client), { policy: f.policy(), today: f.TODAY });
    assert.deepEqual(types(drift), ['budget_changed']);

    var result = lib.apply(drift[0].remediation, t.client, { requestNumber: 'DR-0001', allowStale: true });
    assert.equal(result.ok, true, JSON.stringify(result.results));
    assert.equal(t.mock.state.budgets[0].budget_amount, 1000);
    assert.deepEqual(lib.detectDrift(f.desired(), lib.observe(t.client), { policy: f.policy(), today: f.TODAY }), []);
    assert.match(t.changeLog.entries[t.changeLog.entries.length - 1].operation, /^op-1: Restore budget/);
});

test('remediating a missing cost center and a membership change through apply() restores GitHub', function () {
    var t = h.setup({ supportTeams: true });
    var REQ = { requestNumber: 'RITM0200' };
    lib.apply(lib.plan(f.desired(), lib.observe(t.client), f.policy(), f.TODAY), t.client, { requestNumber: 'RITM0100', allowStale: true });
    var platform = t.client.listCostCenters().filter(function (c) { return c.name === 'Platform'; })[0];
    t.client.removeResources(platform.id, { users: ['linus'] }, { requestNumber: 'GITHUB-UI' });
    var drift = lib.detectDrift(f.desired(), lib.observe(t.client), { policy: f.policy(), today: f.TODAY });
    assert.deepEqual(types(drift), ['membership_changed']);
    assert.equal(lib.apply(drift[0].remediation, t.client, { requestNumber: 'DR-0002', allowStale: true }).ok, true);
    assert.deepEqual(lib.detectDrift(f.desired(), lib.observe(t.client), { policy: f.policy(), today: f.TODAY }), []);
});

test('defence in depth: even if the mode cap were raised, a fix that needs approval never auto-remediates', function () {
    var o = f.observedMatching();
    o.costCenters[1].resources = o.costCenters[1].resources.filter(function (r) { return r.name !== 'linus'; });
    var original = lib.DRIFT_MAX_MODE.membership_changed;
    lib.DRIFT_MAX_MODE.membership_changed = 'auto';
    try {
        var d = detect(o, { modes: { defaults: { membership_changed: 'auto' } } })[0];
        assert.equal(d.mode, 'auto');
        assert.equal(d.remediation.requiresApproval, true);
        assert.equal(d.autoRemediate, false);
    } finally {
        lib.DRIFT_MAX_MODE.membership_changed = original;
    }
});
