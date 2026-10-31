'use strict';

var errors = require('./errors');

var SECONDARY_LIMIT_WAIT_MS = 60000;
var RETRYABLE_STATUS = { 429: true, 502: true, 503: true, 504: true };

function num(v) {
    var n = parseInt(v, 10);
    return isNaN(n) ? null : n;
}

/* Milliseconds the server asked us to wait, or null when it did not say. */
function serverDelayMs(resp, nowMs) {
    var h = resp.headers || {};
    var retryAfter = num(h['retry-after']);
    if (retryAfter !== null) {
        return retryAfter * 1000;
    }
    var reset = num(h['x-ratelimit-reset']);
    if (reset !== null && h['x-ratelimit-remaining'] === '0') {
        return Math.max(0, reset * 1000 - nowMs);
    }
    return null;
}

/*
 * A 502, 503 or 504 on POST is ambiguous: GitHub may have created the object
 * before the gateway gave up, and a retry would create a second one. Rate
 * limits (429, limited 403) are always safe to retry because the request was
 * rejected before it ran. Callers recover from an ambiguous POST failure by
 * reading back (see createCostCenter and createBudget).
 */
function shouldRetry(resp, method) {
    if (errors.isRateLimited(resp)) {
        return true;
    }
    if (String(method || '').toUpperCase() === 'POST') {
        return false;
    }
    return RETRYABLE_STATUS[resp.status] === true;
}

/*
 * Sends a request through the transport with exponential backoff.
 *
 * opts: maxAttempts, baseDelayMs, maxDelayMs, sleep(ms), now(), onThrottle(info)
 * Returns the final response (which may still be an error status; the caller
 * maps it). Throws THROTTLED immediately when the server asks for a wait
 * longer than maxDelayMs, so sync jobs can pause instead of blocking.
 */
function send(transport, req, opts) {
    var attempt = 0;
    var resp;
    while (true) {
        attempt++;
        resp = transport(req);
        if (resp.status < 400 || !shouldRetry(resp, req.method)) {
            return { response: resp, attempts: attempt };
        }
        var wait = serverDelayMs(resp, opts.now());
        /*
         * A rate-limited 403 with no wait hint is GitHub's secondary limit, which asks for at least a minute.
         * Backing off for a few seconds would only extend it, so hand the pause to the caller.
         */
        if (wait === null && resp.status === 403 && errors.isRateLimited(resp)) {
            throwThrottled(resp, SECONDARY_LIMIT_WAIT_MS, attempt, opts);
        }
        if (wait !== null && wait > opts.maxDelayMs) {
            throwThrottled(resp, wait, attempt, opts);
        }
        if (attempt >= opts.maxAttempts) {
            if (errors.isRateLimited(resp)) {
                throwThrottled(resp, wait, attempt, opts);
            }
            return { response: resp, attempts: attempt };
        }
        if (wait === null) {
            wait = Math.min(opts.maxDelayMs, opts.baseDelayMs * Math.pow(2, attempt - 1));
        }
        opts.sleep(wait);
    }
}

function throwThrottled(resp, waitMs, attempt, opts) {
    var info = { status: resp.status, retryAfterMs: waitMs, attempts: attempt };
    if (opts.onThrottle) {
        opts.onThrottle(info);
    }
    throw new errors.GHCCError(errors.KINDS.THROTTLED, 'GitHub throttled the request (HTTP ' + resp.status + ')', info);
}

module.exports = {
    send: send,
    shouldRetry: shouldRetry,
    serverDelayMs: serverDelayMs
};
