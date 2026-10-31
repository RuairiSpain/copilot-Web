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

function money(n) {
    return '$' + String(n);
}

module.exports = {
    lower: lower,
    key: key,
    issue: issue,
    sortBy: sortBy,
    stateOf: stateOf,
    money: money
};
