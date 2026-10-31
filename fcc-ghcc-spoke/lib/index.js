'use strict';

var GHCCClient = require('./client');
var errors = require('./errors');
var changelog = require('./changelog');
var endpoints = require('./endpoints');
var plan = require('./plan');
var apply = require('./apply');
var validate = require('./validate');

module.exports = {
    GHCCClient: GHCCClient,
    GHCCError: errors.GHCCError,
    KINDS: errors.KINDS,
    createMemoryChangeLog: changelog.createMemoryChangeLog,
    redact: changelog.redact,
    endpoints: endpoints,
    plan: plan.plan,
    withPolicyDefaults: plan.withDefaults,
    validate: validate.validate,
    apply: apply.apply,
    observe: apply.observe
};
