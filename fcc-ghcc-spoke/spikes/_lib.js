'use strict';

/*
 * Shared harness for Phase 0 spikes. Unlike lib/, this runs only in Node and
 * uses async fetch. Every request and response is recorded to
 * findings/<spike>.json (token never recorded) so lib/endpoints.js and the
 * mock server can be corrected from real data.
 */

var fs = require('node:fs');
var path = require('node:path');
var lib = require('../lib');

var paths = lib.endpoints.paths;
var KEPT_HEADERS = ['retry-after', 'link', 'x-github-request-id', 'x-ratelimit-limit', 'x-ratelimit-remaining', 'x-ratelimit-reset', 'x-ratelimit-resource'];
var REAL_API = 'https://api.github.com';
var LOCAL_HOSTS = { '127.0.0.1': true, 'localhost': true, '[::1]': true };
/* Keys whose values identify a person. Recorded findings keep the shape of these fields but not the value. */
var PERSONAL_KEYS = /^(email|actor|actor_login|actor_ip|actor_location|login|ip|user_agent)$/i;
var EMAIL = /[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/g;

/* Deep copy with personal fields and anything shaped like an email replaced. */
function scrub(value) {
    var out, k;
    if (typeof value === 'string') {
        return value.replace(EMAIL, '[email]');
    }
    if (value === null || typeof value !== 'object') {
        return value;
    }
    if (Array.isArray(value)) {
        return value.map(scrub);
    }
    out = {};
    for (k in value) {
        if (Object.prototype.hasOwnProperty.call(value, k)) {
            out[k] = PERSONAL_KEYS.test(k) && value[k] ? '[personal data removed]' : scrub(value[k]);
        }
    }
    return out;
}

function isLocal(apiBase) {
    var host;
    try {
        host = new URL(apiBase).hostname;
    } catch (err) {
        return false;
    }
    return LOCAL_HOSTS[host] === true || LOCAL_HOSTS['[' + host + ']'] === true;
}

function createContext(env) {
    var e = env || process.env;
    var token = e.GITHUB_TOKEN;
    var enterprise = e.GITHUB_ENTERPRISE;
    var apiBase = (e.GITHUB_API_BASE || REAL_API).replace(/\/$/, '');
    var steps = [];
    var cleanups = [];
    var runId = Date.now().toString(36);

    if (!token || !enterprise) {
        throw new Error('GITHUB_TOKEN and GITHUB_ENTERPRISE are required');
    }
    /* Anything that is not the local mock is treated as real: api.github.com, GHE.com or a GHES host. */
    if (!isLocal(apiBase) && e.SPIKE_CONFIRM_THROWAWAY !== 'yes') {
        throw new Error('Refusing to run against ' + apiBase + '. Spikes create and delete billing objects. ' +
            'Use a throwaway enterprise and set SPIKE_CONFIRM_THROWAWAY=yes, or point GITHUB_API_BASE at the local mock.');
    }

    var ctx = {
        enterprise: enterprise,
        runId: runId,
        steps: steps,

        /* Required spike input, e.g. a team slug that exists in the throwaway enterprise. */
        need: function (name) {
            if (!e[name]) {
                throw new Error(name + ' is required for this spike');
            }
            return e[name];
        },

        optional: function (name, fallback) {
            return e[name] || fallback;
        },

        unique: function (prefix) {
            return 'spike-' + prefix + '-' + runId;
        },

        sleep: function (ms) {
            return new Promise(function (resolve) { setTimeout(resolve, ms); });
        },

        note: function (text) {
            steps.push({ note: text });
        },

        /* opts.token sends the request with a different credential, e.g. a GitHub App installation token. */
        call: async function (step, method, urlPath, body, opts) {
            var headers = {
                'Authorization': 'Bearer ' + ((opts && opts.token) || token),
                'Accept': 'application/vnd.github+json',
                'X-GitHub-Api-Version': lib.endpoints.API_VERSION
            };
            var init = { method: method, headers: headers };
            if (body !== undefined) {
                headers['Content-Type'] = 'application/json';
                init.body = JSON.stringify(body);
            }
            var res = await fetch(apiBase + urlPath, init);
            var text = await res.text();
            var parsed = text;
            try {
                parsed = text ? JSON.parse(text) : null;
            } catch (err) {
                /* keep raw text */
            }
            var kept = {};
            KEPT_HEADERS.forEach(function (h) {
                if (res.headers.get(h) !== null) {
                    kept[h] = res.headers.get(h);
                }
            });
            var record = {
                step: step,
                request: { method: method, path: urlPath, body: body === undefined ? undefined : lib.redact(body) },
                response: { status: res.status, headers: kept, body: lib.redact(parsed) }
            };
            steps.push(record);
            return { status: res.status, ok: res.status < 300, body: parsed, headers: kept };
        },

        /* Creates a cost center and registers its deletion. */
        createCostCenter: async function (name) {
            var r = await ctx.call('create cost center ' + name, 'POST', paths.costCenters(enterprise), { name: name });
            if (!r.ok) {
                throw new Error('could not create cost center: HTTP ' + r.status + ' ' + JSON.stringify(r.body));
            }
            ctx.addCleanup('delete cost center ' + name, function () {
                return ctx.call('cleanup: delete cost center ' + name, 'DELETE', paths.costCenter(enterprise, r.body.id));
            });
            return r.body;
        },

        addCleanup: function (description, fn) {
            cleanups.push({ description: description, fn: fn });
        },

        /* Runs cleanups in reverse order; a failing cleanup is recorded, not thrown. */
        cleanup: async function () {
            var item;
            while (cleanups.length) {
                item = cleanups.pop();
                try {
                    await item.fn();
                } catch (err) {
                    steps.push({ note: 'cleanup failed: ' + item.description + ': ' + err.message });
                }
            }
        },

        /* verdict: accepted, rejected or inconclusive. details: spike-specific facts for the findings file. */
        finish: function (name, verdict, summary, details) {
            var dir = e.SPIKE_FINDINGS_DIR || path.join(__dirname, 'findings');
            var result = {
                spike: name,
                runAt: new Date().toISOString(),
                apiBase: apiBase,
                enterprise: enterprise,
                verdict: verdict,
                summary: scrub(summary),
                details: scrub(details || {}),
                steps: scrub(steps)
            };
            fs.mkdirSync(dir, { recursive: true });
            fs.writeFileSync(path.join(dir, name + '.json'), JSON.stringify(result, null, 2) + '\n');
            console.log(name + ': ' + verdict + ' - ' + summary);
            return result;
        }
    };
    return ctx;
}

/* Runs a spike's run() when the file is executed directly. */
function main(run) {
    run(process.env).catch(function (err) {
        console.error(err.message);
        process.exit(1);
    });
}

module.exports = { createContext: createContext, main: main, scrub: scrub, isLocal: isLocal };
