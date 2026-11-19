'use strict';

var test = require('node:test');
var assert = require('node:assert/strict');
var h = require('./helpers');
var mockServer = require('../mock/server');

function paths(mock) {
    return mock.requests.map(function (r) { return r.path; });
}

test('listBudgets follows pagination across 250 budgets at 100 per page', function () {
    var t = h.setup();
    t.mock.seedBudgets(250);
    var all = t.client.listBudgets();
    assert.equal(all.length, 250);
    assert.equal(t.mock.requests.length, 3);
    assert.match(paths(t.mock)[0], /per_page=100&page=1$/);
});

test('listBudgets stops after one request when everything fits on a page', function () {
    var t = h.setup();
    t.mock.seedBudgets(5);
    assert.equal(t.client.listBudgets().length, 5);
    assert.equal(t.mock.requests.length, 1);
});

test('listBudgets ends on a short page when the server sends no has_next_page', function () {
    var pages = { 1: 100, 2: 40 };
    var calls = 0;
    var t = h.setup({}, {
        transport: function (req) {
            calls++;
            var page = parseInt(/[?&]page=(\d+)/.exec(req.path)[1], 10);
            var items = [];
            for (var i = 0; i < pages[page]; i++) { items.push({ id: 'b' + page + '-' + i }); }
            return { status: 200, headers: {}, body: { budgets: items } };
        }
    });
    assert.equal(t.client.listBudgets().length, 140);
    assert.equal(calls, 2);
});

test('createBudget for a user scope requires the user field, checked before any request', function () {
    var t = h.setup();
    assert.throws(function () {
        t.client.createBudget({ budget_scope: 'user', budget_amount: 20, prevent_further_usage: true }, h.REQ);
    }, function (e) { return e.kind === 'validation' && /user is required/.test(e.message); });
    assert.equal(t.mock.requests.length, 0);
    assert.equal(t.changeLog.entries.length, 0);
});

test('the mock itself rejects a user budget with no user (contract for the undocumented field)', function () {
    var t = h.setup();
    var transport = mockServer.inProcessTransport(t.mock);
    var resp = transport({ method: 'POST', path: '/enterprises/acme/settings/billing/budgets',
        body: { budget_scope: 'user', budget_amount: 20, prevent_further_usage: true } });
    assert.equal(resp.status, 422);
    assert.deepEqual(resp.body.errors, [{ field: 'user', code: 'missing' }]);
});

test('createBudget is idempotent on scope and entity', function () {
    var t = h.setup();
    var input = { budget_scope: 'cost_center', budget_entity_name: 'Platform', budget_amount: 500, prevent_further_usage: true };
    var first = t.client.createBudget(input, h.REQ);
    assert.equal(first.created, true);
    var again = t.client.createBudget(input, h.REQ);
    assert.equal(again.created, false);
    assert.equal(again.differs, false);
    var changed = t.client.createBudget({ budget_scope: 'cost_center', budget_entity_name: 'Platform', budget_amount: 900, prevent_further_usage: true }, h.REQ);
    assert.equal(changed.created, false);
    assert.equal(changed.differs, true);
    var posts = t.mock.requests.filter(function (r) { return r.method === 'POST'; });
    assert.equal(posts.length, 1);
});

test('updateBudget and deleteBudget write through the gate; delete of a missing budget is not an error', function () {
    var t = h.setup();
    var created = t.client.createBudget({ budget_scope: 'enterprise', budget_amount: 1000, prevent_further_usage: true }, h.REQ);
    var id = created.budget.id;
    t.client.updateBudget(id, { budget_amount: 1500 }, h.REQ);
    assert.equal(t.client.getBudget(id).budget_amount, 1500);
    assert.deepEqual(t.client.deleteBudget(id, h.REQ), { deleted: true });
    assert.deepEqual(t.client.deleteBudget(id, h.REQ), { deleted: false });
});

test('getBudgetUserStates returns what the server reports', function () {
    var t = h.setup();
    var b = t.client.createBudget({ budget_scope: 'multi_user', budget_entity_name: 'Platform users', budget_amount: 100, prevent_further_usage: true }, h.REQ).budget;
    t.mock.setUserStates(b.id, [{ user: 'ada', spent: 100, blocked: true }]);
    assert.deepEqual(t.client.getBudgetUserStates(b.id).users, [{ user: 'ada', spent: 100, blocked: true }]);
});
