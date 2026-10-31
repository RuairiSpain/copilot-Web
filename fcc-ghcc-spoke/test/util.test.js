'use strict';

var test = require('node:test');
var assert = require('node:assert/strict');
var crypto = require('node:crypto');
var util = require('../lib/util');

test('sha256 matches node:crypto for empty, short, block-boundary, long and non-ASCII input', function () {
    var inputs = ['', 'abc', new Array(56).join('a'), new Array(57).join('a'), new Array(65).join('a'), new Array(1001).join('xy'), 'caf\u00e9 \u20ac \ud83d\ude80'];
    inputs.forEach(function (text) {
        assert.equal(util.sha256(text), crypto.createHash('sha256').update(text, 'utf8').digest('hex'));
    });
});

test('fingerprint is a SHA-256 of the canonical JSON, independent of key order', function () {
    assert.equal(util.fingerprint({ a: 1, b: [2, 3] }), util.fingerprint({ b: [2, 3], a: 1 }));
    assert.match(util.fingerprint({ a: 1 }), /^[0-9a-f]{64}$/);
    assert.notEqual(util.fingerprint({ a: 1 }), util.fingerprint({ a: 2 }));
});
