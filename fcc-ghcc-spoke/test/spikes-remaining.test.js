'use strict';

var test = require('node:test');
var assert = require('node:assert/strict');
var withMock = require('./helpers').withMock;
var orgAsResource = require('../spikes/org-as-resource');
var orgVsRepo = require('../spikes/org-vs-repo-precedence');
var expiry = require('../spikes/override-expiry-field');
var userStates = require('../spikes/user-state-response');
var azure = require('../spikes/azure-subscription-api');
var rateLimits = require('../spikes/rate-limits');
var auditLog = require('../spikes/audit-log-actor');
var appAuth = require('../spikes/github-app-auth');

function clean(mock) {
    assert.equal(mock.state.budgets.length, 0, 'budgets left behind');
    assert.equal(mock.state.costCenters.filter(function (c) { return c.state === 'active'; }).length, 0, 'cost centers left behind');
}

test('org-as-resource: org and a finer user assignment coexist', async function () {
    await withMock({}, async function (mock, env) {
        var r = await orgAsResource.run(env);
        assert.equal(r.verdict, 'accepted');
        assert.equal(r.details.coexist, true);
        assert.deepEqual(r.details.resourcesA, ['Org:acme']);
        assert.deepEqual(r.details.resourcesB, ['User:ada']);
        assert.match(r.details.manualStep, /usage report/);
        clean(mock);
    });
});

test('org-vs-repo-precedence: org in one cost center and its repo in another are both stored', async function () {
    await withMock({}, async function (mock, env) {
        var r = await orgVsRepo.run(env);
        assert.equal(r.verdict, 'accepted');
        assert.equal(r.details.orgStoredInA, true);
        assert.equal(r.details.repoStoredInB, true);
        clean(mock);
    });
});

test('override-expiry-field: a field that is accepted but not stored counts as rejected', async function () {
    await withMock({}, async function (mock, env) {
        var r = await expiry.run(env);
        assert.equal(r.verdict, 'rejected');
        assert.equal(r.details.results.expires_at, 'accepted but ignored');
        clean(mock);
    });
});

test('override-expiry-field: a stored field is found and reported', async function () {
    await withMock({ supportBudgetExpiry: true }, async function (mock, env) {
        var r = await expiry.run(env);
        assert.equal(r.verdict, 'accepted');
        assert.equal(r.details.field, 'expires_at');
        clean(mock);
    });
});

test('user-state-response: saves the first path that answers, with its sample', async function () {
    await withMock({}, async function (mock, env) {
        var r = await userStates.run(env);
        assert.equal(r.verdict, 'accepted');
        assert.match(r.details.path, /\/budgets\/bud-\d+\/user-states$/);
        assert.ok(Array.isArray(r.details.sample.users));
        clean(mock);
    });
});

test('user-state-response: reports rejection when every candidate path fails', async function () {
    await withMock({}, async function (mock, env) {
        mock.addFault({ status: 404, method: 'GET', pathIncludes: 'user-states', times: 1 });
        mock.addFault({ status: 404, method: 'GET', pathIncludes: 'user_states', times: 1 });
        mock.addFault({ status: 404, method: 'GET', pathIncludes: '/users', times: 1 });
        var r = await userStates.run(env);
        assert.equal(r.verdict, 'rejected');
        assert.equal(r.details.tried.length, 3);
        clean(mock);
    });
});

test('azure-subscription-api: placeholder plus nothing stored is inconclusive, not a rejection', async function () {
    await withMock({}, async function (mock, env) {
        var r = await azure.run(env);
        assert.equal(r.verdict, 'inconclusive');
        assert.equal(r.details.usedPlaceholder, true);
        clean(mock);
    });
});

test('azure-subscription-api: a real subscription that is not stored is a rejection', async function () {
    await withMock({}, async function (mock, env) {
        env.SPIKE_AZURE_SUBSCRIPTION = '11111111-2222-3333-4444-555555555555';
        var r = await azure.run(env);
        assert.equal(r.verdict, 'rejected');
        clean(mock);
    });
});

test('azure-subscription-api: a stored field is reported once, by name', async function () {
    await withMock({ supportAzureSubscription: true }, async function (mock, env) {
        env.SPIKE_AZURE_SUBSCRIPTION = '11111111-2222-3333-4444-555555555555';
        var r = await azure.run(env);
        assert.equal(r.verdict, 'accepted');
        assert.equal(r.details.field, 'azure_subscription');
        assert.equal(r.details.results.azure_subscription_id, undefined);
        clean(mock);
    });
});

test('rate-limits: stops at the first rate-limit response and reports limit and cost per call', async function () {
    await withMock({ rateLimit: 5 }, async function (mock, env) {
        env.SPIKE_RATE_CALLS = '10';
        var r = await rateLimits.run(env);
        assert.equal(r.verdict, 'accepted');
        assert.equal(r.details.limit, 5);
        assert.equal(r.details.hitLimit.atCall, 6);
        assert.equal(r.details.costPerCall, 1);
        assert.equal(r.details.resource, 'billing');
    });
});

test('rate-limits: reports the limit without exhausting it when calls run out first', async function () {
    await withMock({ rateLimit: 100 }, async function (mock, env) {
        env.SPIKE_RATE_CALLS = '4';
        var r = await rateLimits.run(env);
        assert.equal(r.verdict, 'accepted');
        assert.equal(r.details.hitLimit, null);
        assert.equal(r.details.callsMade, 4);
    });
});

test('rate-limits: no headers at all is inconclusive', async function () {
    await withMock({}, async function (mock, env) {
        env.SPIKE_RATE_CALLS = '3';
        var r = await rateLimits.run(env);
        assert.equal(r.verdict, 'inconclusive');
    });
});

test('audit-log-actor: finds events for the objects it changed, with the actor', async function () {
    await withMock({}, async function (mock, env) {
        env.SPIKE_AUDIT_WAIT_SECONDS = '0';
        var r = await auditLog.run(env);
        assert.equal(r.verdict, 'accepted');
        assert.ok(r.details.events.length >= 4);
        assert.equal(r.details.events[0].actor, '[personal data removed]', 'the actor is recorded as present, not by name');
        clean(mock);
    });
});

test('audit-log-actor: an empty log is inconclusive, a refused log is rejected', async function () {
    await withMock({}, async function (mock, env) {
        env.SPIKE_AUDIT_WAIT_SECONDS = '0';
        env.SPIKE_AUDIT_ONLY = 'yes';
        var r = await auditLog.run(env);
        assert.equal(r.verdict, 'inconclusive');
    });
    await withMock({}, async function (mock, env) {
        env.SPIKE_AUDIT_WAIT_SECONDS = '0';
        env.SPIKE_AUDIT_ONLY = 'yes';
        mock.addFault({ status: 403, method: 'GET', pathIncludes: 'audit-log', times: 1 });
        var r = await auditLog.run(env);
        assert.equal(r.verdict, 'rejected');
        assert.match(r.details.refused, /HTTP 403/);
    });
});

test('github-app-auth: an app token that works everywhere is accepted and leaves nothing behind', async function () {
    await withMock({}, async function (mock, env) {
        env.SPIKE_APP_TOKEN = env.GITHUB_TOKEN;
        var r = await appAuth.run(env);
        assert.equal(r.verdict, 'accepted');
        clean(mock);
    });
});

test('github-app-auth: read-only access is inconclusive; a refused token is rejected', async function () {
    await withMock({}, async function (mock, env) {
        env.SPIKE_APP_TOKEN = env.GITHUB_TOKEN;
        mock.addFault({ status: 403, method: 'POST', times: 1 });
        var r = await appAuth.run(env);
        assert.equal(r.verdict, 'inconclusive');
        assert.equal(r.details.results.createCostCenter, 403);
    });
    await withMock({}, async function (mock, env) {
        env.SPIKE_APP_TOKEN = 'wrong-token';
        var r = await appAuth.run(env);
        assert.equal(r.verdict, 'rejected');
        assert.equal(r.details.results.listCostCenters, 401);
        assert.ok(JSON.stringify(r).indexOf('wrong-token') === -1);
    });
});
