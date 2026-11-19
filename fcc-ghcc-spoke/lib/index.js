'use strict';

var GHCCClient = require('./client');
var errors = require('./errors');
var changelog = require('./changelog');
var endpoints = require('./endpoints');

module.exports = {
    GHCCClient: GHCCClient,
    GHCCError: errors.GHCCError,
    KINDS: errors.KINDS,
    createMemoryChangeLog: changelog.createMemoryChangeLog,
    redact: changelog.redact,
    endpoints: endpoints
};
