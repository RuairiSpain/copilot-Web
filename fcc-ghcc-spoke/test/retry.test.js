'use strict';

var test = require('node:test');
var assert = require('node:assert/strict');
var h = require('./helpers');

test('retries 503 with exponential backoff, then succeeds', function () {
    var t = h.setup({}, { baseDelayMs: 100 });
    t.mock.addFault({ status: 503, method: 'GET', times: 2 });
    var list = t.client.listCostCenters();
    assert.deepEqual(list, []);
    assert.deepEqual(t.sleeps, [100, 200]);
    assert.equal(t.mock.requests.length, 3);
});

test('honours retry-after on 429', function () {
    var t = h.setup({}, { baseDelayMs: 100 });
    t.mock.addFault({ status: 429, method: 'GET', headers: { 'retry-after': '3' }, times: 1 });
    t.client.listCostCenters();
    assert.deepEqual(t.sleeps, [3000]);
});

test('treats 403 with retry-after as a secondary rate limit and retries', function () {
    var t = h.setup({}, { baseDelayMs: 100 });
    t.mock.addFault({ status: 403, method: 'GET', headers: { 'retry-after': '2' }, times: 1 });
    t.client.listCostCenters();
    assert.deepEqual(t.sleeps, [2000]);
});

test('does not retry a plain 403 or 404', function () {
    var t = h.setup();
    t.mock.addFault({ status: 403, method: 'GET', times: 1 });
    assert.throws(function () { t.client.listCostCenters(); }, function (e) { return e.kind === 'credential'; });
    assert.equal(t.mock.requests.length, 1);
    assert.throws(function () { t.client.getCostCenter('nope'); }, function (e) { return e.kind === 'not_found'; });
});

test('gives up after maxAttempts and maps 5xx to transient', function () {
    var t = h.setup({}, { maxAttempts: 3, baseDelayMs: 10 });
    t.mock.addFault({ status: 502, method: 'GET', times: 10 });
    assert.throws(function () { t.client.listCostCenters(); }, function (e) { return e.kind === 'transient' && e.status === 502; });
    assert.equal(t.mock.requests.length, 3);
});

test('throws throttled without sleeping when the server asks for more than maxDelayMs, and signals onThrottle', function () {
    var seen = [];
    var t = h.setup({}, { maxDelayMs: 5000, onThrottle: function (info) { seen.push(info); } });
    t.mock.addFault({ status: 429, method: 'GET', headers: { 'retry-after': '600' }, times: 1 });
    assert.throws(function () { t.client.listCostCenters(); }, function (e) { return e.kind === 'throttled'; });
    assert.deepEqual(t.sleeps, []);
    assert.equal(seen.length, 1);
    assert.equal(seen[0].retryAfterMs, 600000);
});

test('maps 401 to a credential error', function () {
    var t = h.setup();
    var bad = new (require('../lib').GHCCClient)({
        enterprise: 'acme',
        transport: require('../mock/server').inProcessTransport(t.mock, 'wrong')
    });
    assert.throws(function () { bad.listCostCenters(); }, function (e) { return e.kind === 'credential' && e.status === 401; });
});
