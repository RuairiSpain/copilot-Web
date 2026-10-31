'use strict';

var test = require('node:test');
var assert = require('node:assert/strict');
var fs = require('node:fs');
var path = require('node:path');
var withMock = require('./helpers').withMock;
var harness = require('../spikes/_lib');
var teams = require('../spikes/teams-as-resources');
var property = require('../spikes/property-to-assignment');
var overlap = require('../spikes/budget-overlap');

function activeCostCenters(mock) {
    return mock.state.costCenters.filter(function (c) { return c.state === 'active'; });
}

test('teams spike: rejected when the server refuses teams, and cleans up', async function () {
    await withMock({}, async function (mock, env, dir) {
        var result = await teams.run(env);
        assert.equal(result.verdict, 'rejected');
        assert.match(result.summary, /expand teams to direct user resources/);
        assert.equal(activeCostCenters(mock).length, 0);
        assert.ok(fs.existsSync(path.join(dir, 'teams-as-resources.json')));
    });
});

test('teams spike: accepted when the server takes teams, and records what was stored', async function () {
    await withMock({ supportTeams: true }, async function (mock, env) {
        var result = await teams.run(env);
        assert.equal(result.verdict, 'accepted');
        assert.equal(result.details.key, 'teams');
        assert.deepEqual(result.details.resourcesAfter, [{ type: 'Team', name: 'platform' }]);
        assert.equal(activeCostCenters(mock).length, 0);
    });
});

test('property spike: detects manual assignment, then proves explicit add works', async function () {
    await withMock({}, async function (mock, env) {
        var result = await property.run(env);
        assert.equal(result.verdict, 'rejected');
        assert.equal(result.details.automatic, false);
        assert.equal(result.details.explicitAddWorked, true);
        assert.equal(activeCostCenters(mock).length, 0);
    });
});

test('property spike: detects automatic assignment and restores the original property', async function () {
    await withMock({ autoAssignFromProperty: true }, async function (mock, env) {
        mock.state.properties.values['acme/api'] = { cost_center: 'original-value' };
        var result = await property.run(env);
        assert.equal(result.verdict, 'accepted');
        assert.equal(result.details.automatic, true);
        assert.equal(mock.state.properties.values['acme/api'].cost_center, 'original-value');
    });
});

test('budget spike: records overlap and duplicate behaviour, then deletes what it made', async function () {
    await withMock({}, async function (mock, env) {
        var result = await overlap.run(env);
        assert.equal(result.verdict, 'inconclusive');
        assert.equal(result.details.costCenterBudget, 'accepted');
        assert.equal(result.details.userBudget, 'accepted with user field only');
        assert.equal(result.details.enterpriseBudget, 'accepted');
        assert.equal(result.details.duplicateAccepted, true);
        assert.equal(result.details.listHasNextPageFlag, true);
        assert.equal(mock.state.budgets.length, 0);
        assert.equal(activeCostCenters(mock).length, 0);
    });
});

test('budget spike with SPIKE_KEEP leaves the budgets and cost center in place', async function () {
    await withMock({}, async function (mock, env) {
        env.SPIKE_KEEP = 'yes';
        await overlap.run(env);
        assert.equal(mock.state.budgets.length, 4);
        assert.equal(activeCostCenters(mock).length, 1);
    });
});

test('findings files never contain the token', async function () {
    await withMock({}, async function (mock, env, dir) {
        await teams.run(env);
        await overlap.run(env);
        fs.readdirSync(dir).forEach(function (f) {
            assert.ok(fs.readFileSync(path.join(dir, f), 'utf8').indexOf('spike-secret-token') === -1, f);
        });
    });
});

test('spikes refuse to run against api.github.com without explicit confirmation', function () {
    assert.throws(function () {
        harness.createContext({ GITHUB_TOKEN: 't', GITHUB_ENTERPRISE: 'acme' });
    }, /Refusing to run against api\.github\.com/);
    assert.doesNotThrow(function () {
        harness.createContext({ GITHUB_TOKEN: 't', GITHUB_ENTERPRISE: 'acme', SPIKE_CONFIRM_THROWAWAY: 'yes' });
    });
});

test('a spike names the input it is missing', async function () {
    await withMock({}, async function (mock, env) {
        delete env.SPIKE_TEAM;
        await assert.rejects(teams.run(env), /SPIKE_TEAM is required/);
    });
});
