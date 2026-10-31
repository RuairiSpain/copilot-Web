'use strict';

var test = require('node:test');
var assert = require('node:assert/strict');
var mockServer = require('../mock/server');

test('the HTTP wrapper serves the same handler, enforces auth and supports fault injection', async function () {
    var mock = mockServer.createMock({ token: 'tok' });
    var server = await mockServer.startServer(mock, 0);
    var base = 'http://127.0.0.1:' + server.address().port;
    var auth = { authorization: 'Bearer tok', 'content-type': 'application/json' };
    try {
        var noAuth = await fetch(base + '/enterprises/acme/settings/billing/cost-centers');
        assert.equal(noAuth.status, 401);

        var created = await fetch(base + '/enterprises/acme/settings/billing/cost-centers', {
            method: 'POST', headers: auth, body: JSON.stringify({ name: 'Platform' })
        });
        assert.equal(created.status, 201);

        await fetch(base + '/__mock/fault', { method: 'POST', body: JSON.stringify({ status: 429, headers: { 'retry-after': '1' } }) });
        var throttled = await fetch(base + '/enterprises/acme/settings/billing/cost-centers', { headers: auth });
        assert.equal(throttled.status, 429);
        assert.equal(throttled.headers.get('retry-after'), '1');

        var list = await (await fetch(base + '/enterprises/acme/settings/billing/cost-centers', { headers: auth })).json();
        assert.equal(list.costCenters.length, 1);

        var bad = await fetch(base + '/enterprises/acme/settings/billing/budgets', { method: 'POST', headers: auth, body: '{not json' });
        assert.equal(bad.status, 400);

        await fetch(base + '/__mock/reset', { method: 'POST' });
        var empty = await (await fetch(base + '/enterprises/acme/settings/billing/cost-centers', { headers: auth })).json();
        assert.equal(empty.costCenters.length, 0);
    } finally {
        server.close();
    }
});
