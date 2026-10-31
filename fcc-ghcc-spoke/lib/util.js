'use strict';

/* Small helpers shared by the plan engine. ES5 only. */

/*
 * A map for keys that come from outside (logins, team slugs, cost center names, property values).
 * A plain {} inherits constructor, toString and similar, so a user called "constructor" would
 * read as present. These maps have no prototype.
 */
function dict() {
    return Object.create(null);
}

/* obj[key] only when it is the object's own property; undefined otherwise. */
function own(obj, key) {
    return obj && Object.prototype.hasOwnProperty.call(obj, key) ? obj[key] : undefined;
}

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

var SHA256_K = [
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
];

/* UTF-8 bytes of a string, without TextEncoder or Buffer (neither exists in a ServiceNow script). */
function utf8Bytes(text) {
    var out = [];
    var i, c, d;
    for (i = 0; i < text.length; i++) {
        c = text.charCodeAt(i);
        if (c >= 0xd800 && c < 0xdc00 && i + 1 < text.length) {
            d = text.charCodeAt(i + 1);
            if (d >= 0xdc00 && d < 0xe000) {
                c = 0x10000 + ((c - 0xd800) << 10) + (d - 0xdc00);
                i++;
            }
        }
        if (c < 0x80) {
            out.push(c);
        } else if (c < 0x800) {
            out.push(0xc0 | (c >> 6), 0x80 | (c & 63));
        } else if (c < 0x10000) {
            out.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
        } else {
            out.push(0xf0 | (c >> 18), 0x80 | ((c >> 12) & 63), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
        }
    }
    return out;
}

function rotr(x, n) {
    return (x >>> n) | (x << (32 - n));
}

/* SHA-256 as 64 hex characters. Pure ES5, so the same code runs in Node and in a ServiceNow Script Include. */
function sha256(text) {
    var bytes = utf8Bytes(text);
    var bitLength = bytes.length * 8;
    var h = [0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19];
    var w = [];
    var block, i, t, s0, s1, a, b, c, d, e, f, g, hh, ch, maj, t1, t2, hex, v;
    bytes.push(0x80);
    while (bytes.length % 64 !== 56) { bytes.push(0); }
    bytes.push(0, 0, 0, 0, (bitLength >>> 24) & 255, (bitLength >>> 16) & 255, (bitLength >>> 8) & 255, bitLength & 255);
    for (block = 0; block < bytes.length; block += 64) {
        for (i = 0; i < 16; i++) {
            w[i] = (bytes[block + i * 4] << 24) | (bytes[block + i * 4 + 1] << 16) | (bytes[block + i * 4 + 2] << 8) | bytes[block + i * 4 + 3];
        }
        for (i = 16; i < 64; i++) {
            s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >>> 3);
            s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >>> 10);
            w[i] = (w[i - 16] + s0 + w[i - 7] + s1) | 0;
        }
        a = h[0]; b = h[1]; c = h[2]; d = h[3]; e = h[4]; f = h[5]; g = h[6]; hh = h[7];
        for (t = 0; t < 64; t++) {
            ch = (e & f) ^ (~e & g);
            maj = (a & b) ^ (a & c) ^ (b & c);
            t1 = (hh + (rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25)) + ch + SHA256_K[t] + w[t]) | 0;
            t2 = ((rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22)) + maj) | 0;
            hh = g; g = f; f = e; e = (d + t1) | 0; d = c; c = b; b = a; a = (t1 + t2) | 0;
        }
        h[0] = (h[0] + a) | 0; h[1] = (h[1] + b) | 0; h[2] = (h[2] + c) | 0; h[3] = (h[3] + d) | 0;
        h[4] = (h[4] + e) | 0; h[5] = (h[5] + f) | 0; h[6] = (h[6] + g) | 0; h[7] = (h[7] + hh) | 0;
    }
    hex = '';
    for (i = 0; i < 8; i++) {
        v = (h[i] >>> 0).toString(16);
        hex += '00000000'.slice(v.length) + v;
    }
    return hex;
}

/*
 * SHA-256 of the value's canonical JSON. It detects that a plan changed between approval and apply. It does
 * not stop someone who can rewrite both the plan and the stored fingerprint, so keep the approved fingerprint
 * in a record that the plan's editor cannot write.
 */
function fingerprint(value) {
    return sha256(stableStringify(value));
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
    dict: dict,
    own: own,
    lower: lower,
    key: key,
    issue: issue,
    sortBy: sortBy,
    stateOf: stateOf,
    hash: hash,
    distance: distance,
    nearestName: nearestName,
    stableStringify: stableStringify,
    sha256: sha256,
    fingerprint: fingerprint,
    requireToday: requireToday,
    money: money
};
