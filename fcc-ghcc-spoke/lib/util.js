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
    money: money
};
