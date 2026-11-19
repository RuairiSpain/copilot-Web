'use strict';

var fs = require('node:fs');
var os = require('node:os');
var path = require('node:path');
var lib = require('../lib');
var mockServer = require('../mock/server');

/* A client wired to a fresh in-process mock, with a recording sleep and a memory change log. */
function setup(mockOptions, clientOptions) {
    var mock = mockServer.createMock(mockOptions);
    var sleeps = [];
    var changeLog = lib.createMemoryChangeLog();
    var killed = { value: false };
    var config = {
        enterprise: 'acme',
        transport: mockServer.inProcessTransport(mock),
        changeLog: changeLog,
        killSwitch: function () { return killed.value; },
        sleep: function (ms) { sleeps.push(ms); },
        now: function () { return 1000000; }
    };
    var k;
    for (k in clientOptions || {}) {
        config[k] = clientOptions[k];
    }
    return {
        mock: mock,
        client: new lib.GHCCClient(config),
        sleeps: sleeps,
        changeLog: changeLog,
        killed: killed
    };
}

/* Starts the mock over HTTP and gives a spike environment pointing at it, plus a temp findings directory. */
async function withMock(mockOptions, fn) {
    var mock = mockServer.createMock(Object.assign({ token: 'spike-secret-token' }, mockOptions));
    var server = await mockServer.startServer(mock, 0);
    var dir = fs.mkdtempSync(path.join(os.tmpdir(), 'spike-findings-'));
    var env = {
        GITHUB_TOKEN: 'spike-secret-token',
        GITHUB_ENTERPRISE: 'acme',
        GITHUB_API_BASE: 'http://127.0.0.1:' + server.address().port,
        SPIKE_FINDINGS_DIR: dir,
        SPIKE_TEAM: 'platform',
        SPIKE_ORG: 'acme',
        SPIKE_REPO: 'api',
        SPIKE_USER: 'ada',
        SPIKE_POLL_SECONDS: '0'
    };
    try {
        await fn(mock, env, dir);
    } finally {
        server.close();
        fs.rmSync(dir, { recursive: true, force: true });
    }
}

module.exports = { setup: setup, withMock: withMock, REQ: { requestNumber: 'CHG0001' } };
