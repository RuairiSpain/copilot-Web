'use strict';

/*
 * Mock GitHub enterprise billing API.
 *
 * createMock() returns a synchronous handler, so GHCCClient (which is
 * synchronous, as ServiceNow's RESTMessageV2 is) can be tested without
 * HTTP. startServer() wraps the same handler in a node:http server for ATF
 * tests that point the ServiceNow REST Message at it.
 *
 * Response shapes marked ASSUMED in lib/endpoints.js are guesses until the
 * Phase 0 spikes record real responses. Change them here and in lib/ together.
 */

var http = require('node:http');

var BUDGET_SCOPES = ['enterprise', 'organization', 'repository', 'cost_center', 'user', 'multi_user'];

function createMock(options) {
    var opts = options || {};
    var config = {
        token: opts.token || 'test-token',
        maxCostCenters: opts.maxCostCenters || 1000,
        supportTeams: !!opts.supportTeams,
        /* When true, setting the cost_center repo property also assigns the repo, as GitHub might. Phase 0 decides. */
        autoAssignFromProperty: !!opts.autoAssignFromProperty,
        /* Stand-ins for Phase 0 findings: does GitHub store an Azure subscription / a budget expiry? */
        supportAzureSubscription: !!opts.supportAzureSubscription,
        supportBudgetExpiry: !!opts.supportBudgetExpiry,
        /* Requests allowed before a rate-limit response; 0 disables limiting and rate-limit headers. */
        rateLimit: opts.rateLimit || 0,
        maxPerPage: 100
    };
    var state;
    var faults;
    var requests;

    function reset() {
        state = { nextId: 1, calls: 0, costCenters: [], budgets: [], audit: [], properties: { schema: {}, values: {} } };
        faults = [];
        requests = [];
    }
    reset();

    function audit(action, data) {
        var k, event = { '@timestamp': Date.now(), action: action, actor: 'mock-actor' };
        for (k in data || {}) {
            if (Object.prototype.hasOwnProperty.call(data, k)) {
                event[k] = data[k];
            }
        }
        state.audit.push(event);
    }

    function id(prefix) {
        return prefix + '-' + (state.nextId++);
    }

    function reply(status, body, headers) {
        return { status: status, headers: headers || {}, body: body };
    }

    function err(status, message, errors) {
        var body = { message: message };
        if (errors) {
            body.errors = errors;
        }
        return reply(status, body);
    }

    function find(list, wanted) {
        var i;
        for (i = 0; i < list.length; i++) {
            if (list[i].id === wanted) {
                return list[i];
            }
        }
        return null;
    }

    function takeFault(method, path) {
        var i, f;
        for (i = 0; i < faults.length; i++) {
            f = faults[i];
            if ((!f.method || f.method === method) && (!f.pathIncludes || path.indexOf(f.pathIncludes) !== -1)) {
                f.times--;
                if (f.times <= 0) {
                    faults.splice(i, 1);
                }
                return reply(f.status, f.body || { message: 'injected fault' }, f.headers);
            }
        }
        return null;
    }

    /* ---------- cost centers ---------- */

    function activeCount() {
        var n = 0;
        var i;
        for (i = 0; i < state.costCenters.length; i++) {
            if (state.costCenters[i].state === 'active') {
                n++;
            }
        }
        return n;
    }

    function listCostCenters(query) {
        var wanted = query.get('state');
        var out = [];
        var i, c;
        for (i = 0; i < state.costCenters.length; i++) {
            c = state.costCenters[i];
            if (!wanted || c.state === wanted) {
                out.push(c);
            }
        }
        return reply(200, { costCenters: out });
    }

    function createCostCenter(body) {
        var i;
        if (!body || typeof body.name !== 'string' || !body.name) {
            return err(422, 'Validation Failed', [{ field: 'name', code: 'missing' }]);
        }
        for (i = 0; i < state.costCenters.length; i++) {
            if (state.costCenters[i].state === 'active' && state.costCenters[i].name === body.name) {
                return err(409, 'A cost center with this name already exists');
            }
        }
        if (activeCount() >= config.maxCostCenters) {
            return err(422, 'Enterprise has reached the maximum number of cost centers (' + config.maxCostCenters + ')');
        }
        var cc = { id: id('cc'), name: body.name, state: 'active', azure_subscription: null, resources: [] };
        if (config.supportAzureSubscription && body.azure_subscription) {
            cc.azure_subscription = body.azure_subscription;
        }
        state.costCenters.push(cc);
        audit('cost_center.create', { cost_center: cc.name, cost_center_id: cc.id });
        return reply(201, cc);
    }

    function patchCostCenter(cc, body) {
        if (body && typeof body.name === 'string' && body.name) {
            cc.name = body.name;
        }
        if (config.supportAzureSubscription && body && body.azure_subscription) {
            cc.azure_subscription = body.azure_subscription;
        }
        audit('cost_center.update', { cost_center: cc.name, cost_center_id: cc.id });
        return reply(200, cc);
    }

    function deleteCostCenter(cc) {
        audit('cost_center.delete', { cost_center: cc.name, cost_center_id: cc.id });
        cc.state = 'deleted';
        cc.resources = [];
        return reply(200, { message: 'Cost center successfully deleted' });
    }

    function resourceList(body) {
        var out = [];
        var map = { users: 'User', organizations: 'Org', repositories: 'Repo', teams: 'Team' };
        var k, i;
        for (k in map) {
            if (Object.prototype.hasOwnProperty.call(map, k)) {
                for (i = 0; body && body[k] && i < body[k].length; i++) {
                    out.push({ type: map[k], name: body[k][i] });
                }
            }
        }
        return out;
    }

    function hasResource(cc, r) {
        var i;
        for (i = 0; i < cc.resources.length; i++) {
            if (cc.resources[i].type === r.type && cc.resources[i].name === r.name) {
                return i;
            }
        }
        return -1;
    }

    function addResources(cc, body) {
        var reassigned = [];
        var wanted, i, j, other, at;
        if (body && body.teams && !config.supportTeams) {
            return err(422, 'Validation Failed', [{ field: 'teams', code: 'invalid' }]);
        }
        wanted = resourceList(body);
        if (!wanted.length) {
            return err(422, 'Validation Failed', [{ field: 'resources', code: 'missing' }]);
        }
        for (i = 0; i < wanted.length; i++) {
            for (j = 0; j < state.costCenters.length; j++) {
                other = state.costCenters[j];
                if (other !== cc && other.state === 'active') {
                    at = hasResource(other, wanted[i]);
                    if (at !== -1) {
                        other.resources.splice(at, 1);
                        reassigned.push({ type: wanted[i].type, name: wanted[i].name, from: other.id });
                    }
                }
            }
            if (hasResource(cc, wanted[i]) === -1) {
                cc.resources.push(wanted[i]);
            }
        }
        return reply(200, { message: 'Resources successfully added to the cost center.', reassigned_resources: reassigned });
    }

    function removeResources(cc, body) {
        var wanted = resourceList(body);
        var i, at;
        if (!wanted.length) {
            return err(422, 'Validation Failed', [{ field: 'resources', code: 'missing' }]);
        }
        for (i = 0; i < wanted.length; i++) {
            at = hasResource(cc, wanted[i]);
            if (at !== -1) {
                cc.resources.splice(at, 1);
            }
        }
        return reply(200, { message: 'Resources successfully removed from the cost center.' });
    }

    /* ---------- budgets ---------- */

    function validateBudget(body, creating) {
        var problems = [];
        if (creating || body.budget_scope !== undefined) {
            if (BUDGET_SCOPES.indexOf(body.budget_scope) === -1) {
                problems.push({ field: 'budget_scope', code: 'invalid' });
            }
        }
        if (creating || body.budget_amount !== undefined) {
            if (typeof body.budget_amount !== 'number' || body.budget_amount < 0) {
                problems.push({ field: 'budget_amount', code: 'invalid' });
            }
        }
        if (body.prevent_further_usage !== undefined && typeof body.prevent_further_usage !== 'boolean') {
            problems.push({ field: 'prevent_further_usage', code: 'invalid' });
        }
        if (creating && body.budget_scope !== 'enterprise' && !body.budget_entity_name && body.budget_scope !== 'user') {
            problems.push({ field: 'budget_entity_name', code: 'missing' });
        }
        if (creating && body.budget_scope === 'user' && !body.user) {
            problems.push({ field: 'user', code: 'missing' });
        }
        return problems;
    }

    function listBudgets(query) {
        var scope = query.get('scope');
        var perPage = Math.min(parseInt(query.get('per_page') || '30', 10) || 30, config.maxPerPage);
        var page = Math.max(parseInt(query.get('page') || '1', 10) || 1, 1);
        var all = [];
        var i;
        for (i = 0; i < state.budgets.length; i++) {
            if (!scope || state.budgets[i].budget_scope === scope) {
                all.push(state.budgets[i]);
            }
        }
        var slice = all.slice((page - 1) * perPage, page * perPage);
        return reply(200, { budgets: slice, has_next_page: page * perPage < all.length });
    }

    function createBudget(body) {
        var problems = validateBudget(body || {}, true);
        if (problems.length) {
            return err(422, 'Validation Failed', problems);
        }
        var budget = {
            id: id('bud'),
            budget_scope: body.budget_scope,
            budget_entity_name: body.budget_entity_name || '',
            budget_amount: body.budget_amount,
            prevent_further_usage: body.prevent_further_usage === true,
            budget_type: body.budget_type || 'ProductPricing',
            budget_product_sku: body.budget_product_sku || '',
            budget_alerting: body.budget_alerting || { will_alert: false, alert_recipients: [] }
        };
        if (body.user) {
            budget.user = body.user;
        }
        if (config.supportBudgetExpiry && body.expires_at) {
            budget.expires_at = body.expires_at;
        }
        state.budgets.push(budget);
        audit('budget.create', { budget_id: budget.id, budget_scope: budget.budget_scope });
        return reply(201, { message: 'Budget successfully created.', budget: budget });
    }

    function patchBudget(budget, body) {
        var problems = validateBudget(body || {}, false);
        var k;
        if (problems.length) {
            return err(422, 'Validation Failed', problems);
        }
        for (k in body) {
            if (Object.prototype.hasOwnProperty.call(body, k) && k !== 'id') {
                budget[k] = body[k];
            }
        }
        return reply(200, { message: 'Budget successfully updated.', budget: budget });
    }

    function deleteBudget(budget) {
        audit('budget.delete', { budget_id: budget.id });
        state.budgets.splice(state.budgets.indexOf(budget), 1);
        return reply(200, { message: 'Budget successfully deleted.' });
    }

    function userStates(budget) {
        return reply(200, { budget_id: budget.id, users: budget.user_states || [] });
    }

    /* ---------- repository custom properties (used by the Phase 0 property spike) ---------- */

    function setProperties(org, body) {
        var i, j, name, prop, key, i2, cc;
        if (!body || !Array.isArray(body.repository_names) || !Array.isArray(body.properties)) {
            return err(422, 'Validation Failed');
        }
        for (i = 0; i < body.repository_names.length; i++) {
            name = body.repository_names[i];
            key = org + '/' + name;
            state.properties.values[key] = state.properties.values[key] || {};
            for (j = 0; j < body.properties.length; j++) {
                prop = body.properties[j];
                if (prop.value === null) {
                    delete state.properties.values[key][prop.property_name];
                } else {
                    state.properties.values[key][prop.property_name] = prop.value;
                }
                if (config.autoAssignFromProperty && prop.property_name === 'cost_center' && prop.value !== null) {
                    for (i2 = 0; i2 < state.costCenters.length; i2++) {
                        cc = state.costCenters[i2];
                        if (cc.state === 'active' && cc.name === prop.value) {
                            addResources(cc, { repositories: [key] });
                        }
                    }
                }
            }
        }
        return reply(204, undefined);
    }

    function routeProperties(method, url, body) {
        var org = url.pathname.match(/^\/orgs\/([^/]+)\/properties\/(?:schema\/([^/]+)|values)$/);
        var repo = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/properties\/values$/);
        var values, out, k;
        if (org && org[2] && method === 'PUT') {
            state.properties.schema[org[1] + '/' + org[2]] = body || {};
            return reply(200, { property_name: org[2], value_type: (body && body.value_type) || 'string' });
        }
        if (org && !org[2] && method === 'PATCH') {
            return setProperties(org[1], body);
        }
        if (repo && method === 'GET') {
            values = state.properties.values[repo[1] + '/' + repo[2]] || {};
            out = [];
            for (k in values) {
                if (Object.prototype.hasOwnProperty.call(values, k)) {
                    out.push({ property_name: k, value: values[k] });
                }
            }
            return reply(200, out);
        }
        return null;
    }

    /* ---------- routing ---------- */

    function route(method, url, body) {
        var props = routeProperties(method, url, body);
        if (props) {
            return props;
        }
        if (/^\/enterprises\/[^/]+\/audit-log$/.test(url.pathname) && method === 'GET') {
            return reply(200, state.audit);
        }
        var m = url.pathname.match(/^\/enterprises\/([^/]+)\/settings\/billing\/(cost-centers|budgets)(?:\/([^/]+))?(?:\/(resource|user-states))?$/);
        var kind, itemId, sub, item;
        if (!m) {
            return err(404, 'Not Found');
        }
        kind = m[2];
        itemId = m[3];
        sub = m[4];

        if (kind === 'cost-centers') {
            if (!itemId) {
                if (method === 'GET') { return listCostCenters(url.searchParams); }
                if (method === 'POST') { return createCostCenter(body); }
                return err(405, 'Method Not Allowed');
            }
            item = find(state.costCenters, itemId);
            if (!item) { return err(404, 'Not Found'); }
            if (sub === 'resource') {
                if (method === 'POST') { return addResources(item, body); }
                if (method === 'DELETE') { return removeResources(item, body); }
                return err(405, 'Method Not Allowed');
            }
            if (method === 'GET') { return reply(200, item); }
            if (method === 'PATCH') { return patchCostCenter(item, body); }
            if (method === 'DELETE') { return deleteCostCenter(item); }
            return err(405, 'Method Not Allowed');
        }

        if (!itemId) {
            if (method === 'GET') { return listBudgets(url.searchParams); }
            if (method === 'POST') { return createBudget(body); }
            return err(405, 'Method Not Allowed');
        }
        item = find(state.budgets, itemId);
        if (!item) { return err(404, 'Not Found'); }
        if (sub === 'user-states') {
            return method === 'GET' ? userStates(item) : err(405, 'Method Not Allowed');
        }
        if (method === 'GET') { return reply(200, { budget: item }); }
        if (method === 'PATCH') { return patchBudget(item, body); }
        if (method === 'DELETE') { return deleteBudget(item); }
        return err(405, 'Method Not Allowed');
    }

    function handle(req) {
        var method = (req.method || 'GET').toUpperCase();
        var headers = {};
        var k, auth, url, fault;
        for (k in req.headers || {}) {
            if (Object.prototype.hasOwnProperty.call(req.headers, k)) {
                headers[k.toLowerCase()] = req.headers[k];
            }
        }
        requests.push({ method: method, path: req.path, headers: headers, body: req.body });

        auth = headers.authorization || '';
        if (auth !== 'Bearer ' + config.token && auth !== 'token ' + config.token) {
            return err(401, 'Bad credentials');
        }
        url = new URL(req.path, 'http://mock.local');
        state.calls++;
        fault = takeFault(method, url.pathname);
        if (fault) {
            return withRateHeaders(fault);
        }
        if (config.rateLimit && state.calls > config.rateLimit) {
            return withRateHeaders(err(403, 'API rate limit exceeded'));
        }
        return withRateHeaders(route(method, url, req.body));
    }

    function withRateHeaders(out) {
        var k, h = {};
        if (!config.rateLimit) {
            return out;
        }
        for (k in out.headers) {
            if (Object.prototype.hasOwnProperty.call(out.headers, k)) {
                h[k] = out.headers[k];
            }
        }
        h['x-ratelimit-limit'] = String(config.rateLimit);
        h['x-ratelimit-remaining'] = String(Math.max(0, config.rateLimit - state.calls));
        h['x-ratelimit-reset'] = String(Math.floor(Date.now() / 1000) + 3600);
        h['x-ratelimit-resource'] = 'billing';
        return { status: out.status, headers: h, body: out.body };
    }

    return {
        handle: handle,
        reset: reset,
        token: config.token,
        /* fault: { status, method?, pathIncludes?, headers?, body?, times? } */
        addFault: function (fault) {
            faults.push({
                status: fault.status,
                method: fault.method,
                pathIncludes: fault.pathIncludes,
                headers: fault.headers,
                body: fault.body,
                times: fault.times || 1
            });
        },
        seedBudgets: function (n, template) {
            var i, b, k;
            for (i = 0; i < n; i++) {
                b = { id: id('bud'), budget_scope: 'user', budget_entity_name: '', budget_amount: 10, prevent_further_usage: true,
                    budget_type: 'ProductPricing', budget_product_sku: '', user: 'user-' + i };
                for (k in template || {}) {
                    if (Object.prototype.hasOwnProperty.call(template, k)) {
                        b[k] = template[k];
                    }
                }
                state.budgets.push(b);
            }
        },
        setUserStates: function (budgetId, states) {
            find(state.budgets, budgetId).user_states = states;
        },
        get requests() { return requests; },
        get state() { return state; }
    };
}

/* Synchronous transport for GHCCClient that talks to a mock in-process and adds the token, as the REST Message would. */
function inProcessTransport(mock, token) {
    return function (req) {
        var headers = {};
        var k;
        for (k in req.headers || {}) {
            if (Object.prototype.hasOwnProperty.call(req.headers, k)) {
                headers[k] = req.headers[k];
            }
        }
        headers.Authorization = 'Bearer ' + (token === undefined ? mock.token : token);
        return mock.handle({ method: req.method, path: req.path, headers: headers, body: req.body });
    };
}

function startServer(mock, port) {
    var server = http.createServer(function (req, res) {
        var chunks = [];
        req.on('data', function (c) { chunks.push(c); });
        req.on('end', function () {
            var text = Buffer.concat(chunks).toString('utf8');
            var url = new URL(req.url, 'http://mock.local');
            var body;
            var out;
            if (url.pathname === '/__mock/reset' && req.method === 'POST') {
                mock.reset();
                res.writeHead(204);
                res.end();
                return;
            }
            if (url.pathname === '/__mock/fault' && req.method === 'POST') {
                mock.addFault(JSON.parse(text));
                res.writeHead(204);
                res.end();
                return;
            }
            try {
                body = text ? JSON.parse(text) : undefined;
            } catch (e) {
                res.writeHead(400, { 'content-type': 'application/json' });
                res.end(JSON.stringify({ message: 'Problems parsing JSON' }));
                return;
            }
            out = mock.handle({ method: req.method, path: req.url, headers: req.headers, body: body });
            res.writeHead(out.status, Object.assign({ 'content-type': 'application/json; charset=utf-8' }, out.headers));
            res.end(JSON.stringify(out.body));
        });
    });
    return new Promise(function (resolve) {
        server.listen(port || 0, '127.0.0.1', function () {
            resolve(server);
        });
    });
}

module.exports = {
    createMock: createMock,
    inProcessTransport: inProcessTransport,
    startServer: startServer
};

if (require.main === module) {
    var port = parseInt(process.env.MOCK_PORT || '4010', 10);
    var mock = createMock({
        token: process.env.MOCK_TOKEN || 'test-token',
        supportTeams: process.env.MOCK_SUPPORT_TEAMS === '1',
        autoAssignFromProperty: process.env.MOCK_AUTO_ASSIGN === '1'
    });
    startServer(mock, port).then(function () {
        console.log('mock GitHub billing API on http://127.0.0.1:' + port + ' (token: ' + mock.token + ')');
    });
}
