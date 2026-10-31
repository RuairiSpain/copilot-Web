'use strict';

var test = require('node:test');
var assert = require('node:assert/strict');
var h = require('./helpers');

test('createCostCenter is idempotent by name', function () {
    var t = h.setup();
    var a = t.client.createCostCenter({ name: 'Platform' }, h.REQ);
    var b = t.client.createCostCenter({ name: 'Platform' }, h.REQ);
    assert.equal(a.created, true);
    assert.equal(b.created, false);
    assert.equal(b.costCenter.id, a.costCenter.id);
    var posts = t.mock.requests.filter(function (r) { return r.method === 'POST'; });
    assert.equal(posts.length, 1);
    assert.equal(t.changeLog.entries.length, 1);
});

test('adding a resource that is in another cost center moves it (R5 depends on this)', function () {
    var t = h.setup();
    var a = t.client.createCostCenter({ name: 'A' }, h.REQ).costCenter;
    var b = t.client.createCostCenter({ name: 'B' }, h.REQ).costCenter;
    t.client.addResources(a.id, { users: ['ada'], repositories: ['acme/api'] }, h.REQ);
    var moved = t.client.addResources(b.id, { users: ['ada'] }, h.REQ);
    assert.deepEqual(moved.reassigned_resources, [{ type: 'User', name: 'ada', from: a.id }]);
    assert.deepEqual(t.client.getCostCenter(a.id).resources, [{ type: 'Repo', name: 'acme/api' }]);
    assert.deepEqual(t.client.getCostCenter(b.id).resources, [{ type: 'User', name: 'ada' }]);
});

test('removeResources removes only what is listed', function () {
    var t = h.setup();
    var a = t.client.createCostCenter({ name: 'A' }, h.REQ).costCenter;
    t.client.addResources(a.id, { users: ['ada', 'grace'] }, h.REQ);
    t.client.removeResources(a.id, { users: ['ada'] }, h.REQ);
    assert.deepEqual(t.client.getCostCenter(a.id).resources, [{ type: 'User', name: 'grace' }]);
});

test('teams are rejected until a spike shows GitHub accepts them (Phase 0 gate for Phase 2)', function () {
    var t = h.setup();
    var a = t.client.createCostCenter({ name: 'A' }, h.REQ).costCenter;
    assert.throws(function () { t.client.addResources(a.id, { teams: ['platform'] }, h.REQ); },
        function (e) { return e.kind === 'validation' && e.status === 422; });
    var t2 = h.setup({ supportTeams: true });
    var a2 = t2.client.createCostCenter({ name: 'A' }, h.REQ).costCenter;
    t2.client.addResources(a2.id, { teams: ['platform'] }, h.REQ);
});

test('an empty resource set is refused before any request', function () {
    var t = h.setup();
    var a = t.client.createCostCenter({ name: 'A' }, h.REQ).costCenter;
    var before = t.mock.requests.length;
    assert.throws(function () { t.client.addResources(a.id, { users: [] }, h.REQ); }, function (e) { return e.kind === 'validation'; });
    assert.equal(t.mock.requests.length, before);
});

test('the cost center limit is enforced by the mock (configurable)', function () {
    var t = h.setup({ maxCostCenters: 2 });
    t.client.createCostCenter({ name: 'A' }, h.REQ);
    t.client.createCostCenter({ name: 'B' }, h.REQ);
    assert.throws(function () { t.client.createCostCenter({ name: 'C' }, h.REQ); },
        function (e) { return e.kind === 'validation' && /maximum number/.test(e.message); });
});

test('deleted cost centers drop out of the default list and free their resources', function () {
    var t = h.setup();
    var a = t.client.createCostCenter({ name: 'A' }, h.REQ).costCenter;
    t.client.addResources(a.id, { users: ['ada'] }, h.REQ);
    assert.deepEqual(t.client.deleteCostCenter(a.id, h.REQ), { deleted: true });
    assert.equal(t.client.listCostCenters({ state: 'active' }).length, 0);
    assert.equal(t.client.listCostCenters({ state: 'deleted' }).length, 1);
    var b = t.client.createCostCenter({ name: 'B' }, h.REQ).costCenter;
    var res = t.client.addResources(b.id, { users: ['ada'] }, h.REQ);
    assert.deepEqual(res.reassigned_resources, []);
});
