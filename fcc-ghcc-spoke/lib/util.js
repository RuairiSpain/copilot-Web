'use strict';

/* Small helpers shared by the plan engine. ES5 only. */

function lower(s) {
    return String(s === undefined || s === null ? '' : s).toLowerCase();
}

/* Identity of a cost center resource, comparable across ServiceNow and GitHub. */
function key(type, name) {
    return type + ':' + lower(name);
}

function issue(code, message, subject) {
    return { code: code, message: message, subject: subject || null };
}

function sortBy(list, fn) {
    return list.slice().sort(function (a, b) {
        var x = fn(a);
        var y = fn(b);
        return x < y ? -1 : (x > y ? 1 : 0);
    });
}

/* Records without a state are treated as approved; the ServiceNow adapter passes only Approved and Retired records. */
function stateOf(record) {
    return record.state || 'approved';
}

/* Short, stable fingerprint of a string (djb2), used to key drift records. */
function hash(text) {
    var h = 5381;
    var i;
    for (i = 0; i < text.length; i++) {
        h = ((h << 5) + h + text.charCodeAt(i)) | 0;
    }
    return (h >>> 0).toString(36);
}

/* Edit distance, for suggesting the cost center a mistyped property value probably meant. */
function distance(a, b) {
    var prev = [];
    var cur, i, j, cost;
    for (j = 0; j <= b.length; j++) { prev.push(j); }
    for (i = 1; i <= a.length; i++) {
        cur = [i];
        for (j = 1; j <= b.length; j++) {
            cost = a.charAt(i - 1) === b.charAt(j - 1) ? 0 : 1;
            cur.push(Math.min(prev[j] + 1, cur[j - 1] + 1, prev[j - 1] + cost));
        }
        prev = cur;
    }
    return prev[b.length];
}

/*
 * The nearest known name to a mistyped value, within maxDistance edits (default 2).
 * `known` maps lower-case name -> display name. Returns the display name or null.
 */
function nearestName(value, known, maxDistance) {
    var best = null;
    var bestDistance = (maxDistance === undefined ? 2 : maxDistance) + 1;
    var name, d;
    if (!value) { return null; }
    for (name in known) {
        if (Object.prototype.hasOwnProperty.call(known, name)) {
            d = distance(lower(value), name);
            if (d < bestDistance) { bestDistance = d; best = known[name]; }
        }
    }
    return best;
}

/* JSON with object keys in sorted order, so equal values always give equal text. */
function stableStringify(value) {
    var keys;
    if (value === null || typeof value !== 'object') {
        return JSON.stringify(value === undefined ? null : value);
    }
    if (Array.isArray(value)) {
        return '[' + value.map(stableStringify).join(',') + ']';
    }
    keys = Object.keys(value).sort();
    return '{' + keys.map(function (k) { return JSON.stringify(k) + ':' + stableStringify(value[k]); }).join(',') + '}';
}

/* 64-bit change detector (FNV-1a and djb2 side by side). Detects an edited plan; it is not a security hash. */
function fingerprint(value) {
    var text = stableStringify(value);
    var f = 0x811c9dc5;
    var i;
    for (i = 0; i < text.length; i++) {
        f ^= text.charCodeAt(i);
        f = (f + ((f << 1) + (f << 4) + (f << 7) + (f << 8) + (f << 24))) >>> 0;
    }
    return f.toString(16) + hash(text);
}

/* Throws unless `today` is a real YYYY-MM-DD date. A missing date would silently skip expiry and review checks. */
function requireToday(today, where) {
    if (typeof today !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(today)) {
        throw new Error(where + ': today is required as YYYY-MM-DD (the engine never reads a clock)');
    }
    return today;
}

function money(n) {
    return '$' + String(n);
}

module.exports = {
    lower: lower,
    key: key,
    issue: issue,
    sortBy: sortBy,
    stateOf: stateOf,
    hash: hash,
    distance: distance,
    nearestName: nearestName,
    stableStringify: stableStringify,
    fingerprint: fingerprint,
    requireToday: requireToday,
    money: money
};
