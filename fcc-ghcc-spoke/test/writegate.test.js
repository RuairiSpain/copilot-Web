'use strict';

var test = require('node:test');
var assert = require('node:assert/strict');
var h = require('./helpers');
var lib = require('../lib');

test('a write without a request number is refused before any request or log entry', function () {
    var t = h.setup();
    assert.throws(function () { t.client.createCostCenter({ name: 'A' }, {}); }, function (e) { return e.kind === 'precondition'; });
    assert.throws(function () { t.client.createCostCenter({ name: 'A' }); }, function (e) { return e.kind === 'precondition'; });
    var posts = t.mock.requests.filter(function (r) { return r.method === 'POST'; });
    assert.equal(posts.length, 0);
    assert.equal(t.changeLog.entries.length, 0);
});

test('the kill switch halts writes but leaves reads working (X5)', function () {
    var t = h.setup();
    t.client.createCostCenter({ name: 'A' }, h.REQ);
    t.killed.value = true;
    assert.throws(function () { t.client.createCostCenter({ name: 'B' }, h.REQ); }, function (e) { return e.kind === 'kill_switch'; });
    assert.equal(t.client.listCostCenters().length, 1);
    assert.equal(t.changeLog.entries.length, 1);
});

test('a successful write logs request, payload, status and response', function () {
    var t = h.setup();
    t.client.createCostCenter({ name: 'A' }, { requestNumber: 'RITM0042', operation: 'create cost center A' });
    var e = t.changeLog.entries[0];
    assert.equal(e.request, 'RITM0042');
    assert.equal(e.operation, 'create cost center A');
    assert.deepEqual(e.payload, { name: 'A' });
    assert.equal(e.state, 'done');
    assert.equal(e.status, 201);
    assert.equal(e.response.name, 'A');
});

test('a failed write logs the error and rethrows', function () {
    var t = h.setup({ maxCostCenters: 0 + 1 });
    t.client.createCostCenter({ name: 'A' }, h.REQ);
    assert.throws(function () { t.client.createCostCenter({ name: 'B' }, h.REQ); }, function (e) { return e.kind === 'validation'; });
    var e = t.changeLog.entries[1];
    assert.equal(e.state, 'error');
    assert.equal(e.status, 422);
});

test('a throttled write is logged as an error and the pause signal fires', function () {
    var seen = 0;
    var t = h.setup({}, { maxDelayMs: 1000, onThrottle: function () { seen++; } });
    t.mock.addFault({ status: 429, method: 'POST', headers: { 'retry-after': '120' }, times: 1 });
    assert.throws(function () { t.client.createCostCenter({ name: 'A' }, h.REQ); }, function (e) { return e.kind === 'throttled'; });
    assert.equal(seen, 1);
    assert.equal(t.changeLog.entries[0].state, 'error');
});

test('secrets never reach the change log', function () {
    var t = h.setup();
    var budget = t.client.createBudget({
        budget_scope: 'enterprise', budget_amount: 10, prevent_further_usage: true,
        budget_alerting: { will_alert: true, alert_recipients: ['a@example.com'], api_token: 'ghp_SECRET' }
    }, h.REQ);
    assert.ok(budget.created);
    var text = JSON.stringify(t.changeLog.entries);
    assert.ok(text.indexOf('ghp_SECRET') === -1);
    assert.ok(text.indexOf(t.mock.token) === -1);
});

test('redact replaces secret-looking keys at any depth', function () {
    assert.deepEqual(
        lib.redact({ a: 1, nested: [{ Authorization: 'x', ok: true }], password: 'p' }),
        { a: 1, nested: [{ Authorization: '[redacted]', ok: true }], password: '[redacted]' }
    );
});

test('the client constructor requires an enterprise and a transport', function () {
    assert.throws(function () { new lib.GHCCClient({ transport: function () {} }); }, function (e) { return e.kind === 'precondition'; });
    assert.throws(function () { new lib.GHCCClient({ enterprise: 'acme' }); }, function (e) { return e.kind === 'precondition'; });
});
