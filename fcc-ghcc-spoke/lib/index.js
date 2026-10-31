'use strict';

var GHCCClient = require('./client');
var errors = require('./errors');
var changelog = require('./changelog');
var endpoints = require('./endpoints');
var plan = require('./plan');
var apply = require('./apply');
var validate = require('./validate');
var drift = require('./drift');

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
    observe: apply.observe,
    detectDrift: drift.detectDrift,
    reconcileDrift: drift.reconcile,
    DRIFT_TYPES: drift.TYPES,
    DRIFT_MAX_MODE: drift.MAX_MODE,
    DRIFT_RECOMMENDED_MODES: drift.RECOMMENDED_MODES
};
