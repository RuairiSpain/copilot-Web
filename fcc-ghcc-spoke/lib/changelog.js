'use strict';

var SECRET_KEY = /token|secret|password|authorization|credential/i;

/* Returns a deep copy with secret-looking keys replaced. */
function redact(value) {
    var out, i, k;
    if (value === null || typeof value !== 'object') {
        return value;
    }
    if (Array.isArray(value)) {
        out = [];
        for (i = 0; i < value.length; i++) {
            out.push(redact(value[i]));
        }
        return out;
    }
    out = {};
    for (k in value) {
        if (Object.prototype.hasOwnProperty.call(value, k)) {
            out[k] = SECRET_KEY.test(k) ? '[redacted]' : redact(value[k]);
        }
    }
    return out;
}

/*
 * In-memory change log used by tests and the mock. In ServiceNow, an adapter
 * with the same two methods writes x_fcc_ghcc_change_log records.
 */
function createMemoryChangeLog() {
    var entries = [];
    return {
        entries: entries,
        begin: function (entry) {
            var record = {
                id: entries.length + 1,
                request: entry.request,
                operation: entry.operation,
                method: entry.method,
                path: entry.path,
                payload: redact(entry.payload),
                state: 'pending',
                status: null,
                response: null,
                error: null
            };
            entries.push(record);
            return record.id;
        },
        finish: function (id, result) {
            var record = entries[id - 1];
            record.state = result.error ? 'error' : 'done';
            record.status = result.status;
            record.response = redact(result.response);
            record.error = result.error || null;
        }
    };
}

module.exports = {
    redact: redact,
    createMemoryChangeLog: createMemoryChangeLog
};
