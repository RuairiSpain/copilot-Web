'use strict';

var errors = require('./errors');
var retry = require('./retry');
var endpoints = require('./endpoints');

var GHCCError = errors.GHCCError;
var KINDS = errors.KINDS;

/*
 * GHCCClient: the only code that talks to GitHub.
 *
 * config:
 *   enterprise    enterprise slug (required)
 *   transport     function (req) -> { status, headers, body }, synchronous.
 *                 req = { method, path, headers, body }. It adds the
 *                 Authorization header itself; this file never sees the token.
 *   changeLog     { begin(entry) -> id, finish(id, result) } (required for writes)
 *   killSwitch    function () -> boolean; true halts all writes
 *   sleep, now    injected for tests; defaults use real time
 *   pageSize (100), maxPages (1000), maxAttempts (4), baseDelayMs (1000),
 *   maxDelayMs (60000), onThrottle(info)
 */
function GHCCClient(config) {
    if (!config || !config.enterprise) {
        throw new GHCCError(KINDS.PRECONDITION, 'enterprise is required');
    }
    if (typeof config.transport !== 'function') {
        throw new GHCCError(KINDS.PRECONDITION, 'transport is required');
    }
    this.enterprise = config.enterprise;
    this.transport = config.transport;
    this.changeLog = config.changeLog || null;
    this.killSwitch = config.killSwitch || function () { return false; };
    if (typeof config.sleep !== 'function') {
        /* Without a real wait, retries would fire back to back and make rate limiting worse. */
        throw new GHCCError(KINDS.PRECONDITION, 'sleep is required (in ServiceNow, a function that calls gs.sleep(ms))');
    }
    this.pageSize = pick(config.pageSize, 100);
    this.maxPages = pick(config.maxPages, 1000);
    this.retryOpts = {
        maxAttempts: pick(config.maxAttempts, 4),
        baseDelayMs: pick(config.baseDelayMs, 1000),
        maxDelayMs: pick(config.maxDelayMs, 60000),
        sleep: config.sleep,
        now: config.now || function () { return new Date().getTime(); },
        onThrottle: config.onThrottle
    };
}

/* ---------- helpers ---------- */

/* A configured number wins, including 0; only undefined falls back. */
function pick(value, fallback) {
    return value === undefined || value === null ? fallback : value;
}

function lowerKeys(headers) {
    var out = {};
    var k;
    for (k in headers || {}) {
        if (Object.prototype.hasOwnProperty.call(headers, k)) {
            out[k.toLowerCase()] = String(headers[k]);
        }
    }
    return out;
}

function parseBody(body) {
    if (typeof body === 'string' && body.length) {
        try {
            return JSON.parse(body);
        } catch (e) {
            return body;
        }
    }
    return body;
}

function queryString(params) {
    var parts = [];
    var k;
    for (k in params || {}) {
        if (Object.prototype.hasOwnProperty.call(params, k) && params[k] !== undefined && params[k] !== null) {
            parts.push(encodeURIComponent(k) + '=' + encodeURIComponent(params[k]));
        }
    }
    return parts.length ? '?' + parts.join('&') : '';
}

function unwrap(body, key) {
    if (body && typeof body === 'object' && body[key] !== undefined) {
        return body[key];
    }
    return body;
}

function hasLinkNext(headers) {
    return /rel="next"/.test(headers.link || '');
}

GHCCClient.prototype._call = function (method, path, payload) {
    var self = this;
    var req = {
        method: method,
        path: path,
        headers: {
            'accept': 'application/vnd.github+json',
            'x-github-api-version': endpoints.API_VERSION
        },
        body: payload === undefined ? undefined : payload
    };
    var result = retry.send(function (r) {
        var raw = self.transport(r);
        return { status: raw.status, headers: lowerKeys(raw.headers), body: parseBody(raw.body) };
    }, req, this.retryOpts);
    return result.response;
};

GHCCClient.prototype._read = function (method, path) {
    var resp = this._call(method, path);
    if (resp.status >= 400) {
        throw errors.mapError(resp);
    }
    return resp;
};

/*
 * Every write goes through here: precondition, kill switch, change_log
 * before and after, error mapping. Returns the response body.
 * meta: { requestNumber, operation }
 */
GHCCClient.prototype._write = function (method, path, payload, meta) {
    if (!meta || !meta.requestNumber) {
        throw new GHCCError(KINDS.PRECONDITION, 'a request or change number is required for every write');
    }
    if (this.killSwitch()) {
        throw new GHCCError(KINDS.KILL_SWITCH, 'writes are halted by the kill switch');
    }
    if (!this.changeLog) {
        throw new GHCCError(KINDS.PRECONDITION, 'a changeLog is required for writes');
    }
    var logId = this.changeLog.begin({
        request: meta.requestNumber,
        operation: meta.operation || (method + ' ' + path),
        method: method,
        path: path,
        payload: payload
    });
    var resp;
    try {
        resp = this._call(method, path, payload);
    } catch (e) {
        this.changeLog.finish(logId, { status: e.status || 0, response: e.details && e.details.body, error: e.message });
        throw e;
    }
    if (resp.status >= 400) {
        var err = errors.mapError(resp);
        this.changeLog.finish(logId, { status: resp.status, response: resp.body, error: err.message });
        throw err;
    }
    this.changeLog.finish(logId, { status: resp.status, response: resp.body });
    return resp.body;
};

/* Identity of a list item, to notice a server that keeps returning the same page. */
function itemKey(item) {
    return item && item.id !== undefined ? 'id:' + item.id : 'json:' + JSON.stringify(item);
}

GHCCClient.prototype._list = function (path, itemsKey, params, paginate) {
    var items = [];
    var seen = Object.create(null);
    var page = 1;
    var self = this;
    while (true) {
        var q = {};
        var k;
        for (k in params || {}) {
            if (Object.prototype.hasOwnProperty.call(params, k)) {
                q[k] = params[k];
            }
        }
        if (paginate) {
            q.per_page = self.pageSize;
            q.page = page;
        }
        var resp = self._read('GET', path + queryString(q));
        var body = resp.body;
        var pageItems = Array.isArray(body) ? body : ((body && body[itemsKey]) || []);
        var i, fresh = 0, k;
        for (i = 0; i < pageItems.length; i++) {
            k = itemKey(pageItems[i]);
            if (!seen[k]) {
                seen[k] = true;
                fresh++;
                items.push(pageItems[i]);
            }
        }
        if (!paginate) {
            return items;
        }
        var more;
        if (body && typeof body.has_next_page === 'boolean') {
            more = body.has_next_page;
        } else if (resp.headers.link) {
            more = hasLinkNext(resp.headers);
        } else {
            /*
             * No signal from the server. A short page does not prove the last one, because the server may cap the
             * page size. Read until a page is empty or adds nothing new; the second case is a server that ignores
             * `page` and returns the same list every time, which would otherwise run to maxPages.
             */
            more = fresh > 0;
        }
        if (!more) {
            return items;
        }
        page++;
        if (page > self.maxPages) {
            throw new GHCCError(KINDS.UNKNOWN, 'pagination did not end after ' + self.maxPages + ' pages');
        }
    }
};

function findBy(list, predicate) {
    var i;
    for (i = 0; i < list.length; i++) {
        if (predicate(list[i])) {
            return list[i];
        }
    }
    return null;
}

/* ---------- cost centers ---------- */

GHCCClient.prototype.listCostCenters = function (opts) {
    var state = opts && opts.state;
    return this._list(endpoints.paths.costCenters(this.enterprise), endpoints.keys.costCenterList, { state: state }, true);
};

GHCCClient.prototype.getCostCenter = function (id) {
    return this._read('GET', endpoints.paths.costCenter(this.enterprise, id)).body;
};

/* Idempotent: returns the existing active cost center with the same name instead of creating a second. */
GHCCClient.prototype.createCostCenter = function (input, meta) {
    var existing = findBy(this.listCostCenters({ state: 'active' }), function (c) {
        return c.name === input.name;
    });
    if (existing) {
        return { created: false, costCenter: existing };
    }
    var body;
    try {
        body = this._write('POST', endpoints.paths.costCenters(this.enterprise), { name: input.name }, meta);
    } catch (e) {
        /* A gateway error on POST is not retried, because the create may have succeeded. Read back before giving up. */
        if (e.kind !== KINDS.TRANSIENT) {
            throw e;
        }
        existing = findBy(this.listCostCenters({ state: 'active' }), function (c) {
            return c.name === input.name;
        });
        if (!existing) {
            throw e;
        }
        return { created: true, costCenter: existing, recovered: true };
    }
    return { created: true, costCenter: body };
};

GHCCClient.prototype.updateCostCenter = function (id, input, meta) {
    return this._write('PATCH', endpoints.paths.costCenter(this.enterprise, id), input, meta);
};

/* Idempotent: a cost center that is already gone is reported, not an error. */
GHCCClient.prototype.deleteCostCenter = function (id, meta) {
    try {
        this._write('DELETE', endpoints.paths.costCenter(this.enterprise, id), undefined, meta);
        return { deleted: true };
    } catch (e) {
        if (e.kind === KINDS.NOT_FOUND) {
            return { deleted: false };
        }
        throw e;
    }
};

function checkResources(resources) {
    var total = 0;
    var k;
    for (k in resources || {}) {
        if (Object.prototype.hasOwnProperty.call(resources, k)) {
            if (!Array.isArray(resources[k])) {
                throw new GHCCError(KINDS.VALIDATION, 'resources.' + k + ' must be an array');
            }
            total += resources[k].length;
        }
    }
    if (total === 0) {
        throw new GHCCError(KINDS.VALIDATION, 'at least one resource is required');
    }
}

/* resources: { users: [], organizations: [], repositories: [] }. GitHub moves a resource out of any other cost center. */
GHCCClient.prototype.addResources = function (id, resources, meta) {
    checkResources(resources);
    return this._write('POST', endpoints.paths.costCenterResource(this.enterprise, id), resources, meta);
};

GHCCClient.prototype.removeResources = function (id, resources, meta) {
    checkResources(resources);
    return this._write('DELETE', endpoints.paths.costCenterResource(this.enterprise, id), resources, meta);
};

/* ---------- budgets ---------- */

GHCCClient.prototype.listBudgets = function (opts) {
    var params = {};
    if (opts && opts.scope) {
        params.scope = opts.scope;
    }
    return this._list(endpoints.paths.budgets(this.enterprise), endpoints.keys.budgetList, params, true);
};

GHCCClient.prototype.getBudget = function (id) {
    return unwrap(this._read('GET', endpoints.paths.budget(this.enterprise, id)).body, endpoints.keys.budget);
};

GHCCClient.prototype.getBudgetUserStates = function (id) {
    return this._read('GET', endpoints.paths.budgetUserStates(this.enterprise, id)).body;
};

/* existing is what GitHub returned; wanted is the caller's input. Fields the caller left out are not compared. */
function sameBudgetTarget(existing, wanted) {
    var fields = ['budget_scope', 'budget_entity_name', 'budget_type', 'budget_product_sku'];
    var i;
    for (i = 0; i < fields.length; i++) {
        if (wanted[fields[i]] !== undefined && (existing[fields[i]] || '') !== wanted[fields[i]]) {
            return false;
        }
    }
    return true;
}

/*
 * Idempotent on (scope, entity, type, sku). When a matching budget exists the
 * call writes nothing and reports whether its amount or stop-usage differ, so
 * the plan engine can emit an update instead.
 */
GHCCClient.prototype.createBudget = function (input, meta) {
    if (/user/.test(input.budget_scope || '') && input.budget_scope !== 'multi_user' && !input.user) {
        throw new GHCCError(KINDS.VALIDATION, 'user is required for user-scoped budgets (field missing from GitHub docs schema)');
    }
    var existing = findBy(this.listBudgets({ scope: input.budget_scope }), function (b) {
        return sameBudgetTarget(b, input) && (input.user === undefined || b.user === input.user);
    });
    if (existing) {
        return {
            created: false,
            budget: existing,
            differs: existing.budget_amount !== input.budget_amount ||
                existing.prevent_further_usage !== input.prevent_further_usage
        };
    }
    var body;
    try {
        body = this._write('POST', endpoints.paths.budgets(this.enterprise), input, meta);
    } catch (e) {
        /* See createCostCenter: an ambiguous POST failure is resolved by reading back, not by retrying. */
        if (e.kind !== KINDS.TRANSIENT) {
            throw e;
        }
        existing = findBy(this.listBudgets({ scope: input.budget_scope }), function (b) {
            return sameBudgetTarget(b, input) && (input.user === undefined || b.user === input.user);
        });
        if (!existing) {
            throw e;
        }
        return { created: true, budget: existing, recovered: true };
    }
    return { created: true, budget: unwrap(body, endpoints.keys.budget) };
};

GHCCClient.prototype.updateBudget = function (id, input, meta) {
    return this._write('PATCH', endpoints.paths.budget(this.enterprise, id), input, meta);
};

GHCCClient.prototype.deleteBudget = function (id, meta) {
    try {
        this._write('DELETE', endpoints.paths.budget(this.enterprise, id), undefined, meta);
        return { deleted: true };
    } catch (e) {
        if (e.kind === KINDS.NOT_FOUND) {
            return { deleted: false };
        }
        throw e;
    }
};

module.exports = GHCCClient;
