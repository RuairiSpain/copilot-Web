'use strict';

/*
 * Reconciles freshly detected items (drift or anomalies) with the records
 * ServiceNow already holds, so a repeated sync never creates duplicates (X4).
 *
 *   detected     items with a stable `key`
 *   existing     [{ key, state }]
 *   trackedStates  record states that mean "already raised, do not raise again"
 *   openStates     record states that mean "still open", which resolve when the item disappears
 *
 * Returns { create, resolve, unchanged }: items with no record yet, keys of open records whose
 * item has gone, and items already tracked. A record in any other state (for example
 * resolved) does not block the same item being raised again if it recurs.
 */
function reconcileRecords(detected, existing, trackedStates, openStates) {
    var tracked = {};
    var open = {};
    var seen = {};
    var create = [];
    var unchanged = [];
    var resolve = [];
    (existing || []).forEach(function (r) {
        if (trackedStates.indexOf(r.state) !== -1) { tracked[r.key] = true; }
        if (openStates.indexOf(r.state) !== -1) { open[r.key] = true; }
    });
    detected.forEach(function (d) {
        seen[d.key] = true;
        (tracked[d.key] ? unchanged : create).push(d);
    });
    Object.keys(open).forEach(function (k) {
        if (!seen[k]) { resolve.push(k); }
    });
    return { create: create, resolve: resolve.sort(), unchanged: unchanged };
}

module.exports = { reconcileRecords: reconcileRecords };
