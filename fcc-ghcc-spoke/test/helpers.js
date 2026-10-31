'use strict';

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

module.exports = { setup: setup, REQ: { requestNumber: 'CHG0001' } };
