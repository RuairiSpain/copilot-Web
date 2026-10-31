'use strict';

/*
 * Error model for GHCCClient. ES5 only: this code is packaged into a
 * ServiceNow Script Include, so no let/const, arrow functions, classes or
 * template strings.
 */

var KINDS = {
    CREDENTIAL: 'credential',
    NOT_FOUND: 'not_found',
    VALIDATION: 'validation',
    THROTTLED: 'throttled',
    TRANSIENT: 'transient',
    KILL_SWITCH: 'kill_switch',
    PRECONDITION: 'precondition',
    UNKNOWN: 'unknown'
};

function GHCCError(kind, message, details) {
    this.name = 'GHCCError';
    this.kind = kind;
    this.message = message;
    this.details = details || {};
    this.status = this.details.status || 0;
    this.stack = (new Error(message)).stack;
}
GHCCError.prototype = Object.create(Error.prototype);
GHCCError.prototype.constructor = GHCCError;

function header(headers, name) {
    return headers ? headers[name] : undefined;
}

/* GitHub's secondary rate limit answers 403 with a message and sometimes no rate-limit headers. */
function bodyMessage(resp) {
    var b = resp.body;
    return b && typeof b === 'object' && typeof b.message === 'string' ? b.message : '';
}

function isRateLimited(resp) {
    var h = resp.headers || {};
    if (resp.status === 429) {
        return true;
    }
    if (resp.status === 403) {
        return header(h, 'retry-after') !== undefined || header(h, 'x-ratelimit-remaining') === '0' || /rate limit|abuse/i.test(bodyMessage(resp));
    }
    return false;
}

function messageOf(resp) {
    var b = resp.body;
    var text = 'HTTP ' + resp.status;
    if (b && typeof b === 'object' && b.message) {
        text += ': ' + b.message;
    }
    return text;
}

/* Maps a failed HTTP response to a GHCCError. Callers must pass status >= 400. */
function mapError(resp) {
    var details = { status: resp.status, body: resp.body };
    var msg = messageOf(resp);
    if (isRateLimited(resp)) {
        return new GHCCError(KINDS.THROTTLED, msg, details);
    }
    if (resp.status === 401 || resp.status === 403) {
        return new GHCCError(KINDS.CREDENTIAL, msg, details);
    }
    if (resp.status === 404) {
        return new GHCCError(KINDS.NOT_FOUND, msg, details);
    }
    if (resp.status === 409 || resp.status === 422 || resp.status === 400) {
        return new GHCCError(KINDS.VALIDATION, msg, details);
    }
    if (resp.status >= 500) {
        return new GHCCError(KINDS.TRANSIENT, msg, details);
    }
    return new GHCCError(KINDS.UNKNOWN, msg, details);
}

module.exports = {
    KINDS: KINDS,
    GHCCError: GHCCError,
    mapError: mapError,
    isRateLimited: isRateLimited
};
