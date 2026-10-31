'use strict';

/*
 * lib/ is packaged into a ServiceNow Script Include, so it must stay ES5. The repo's ESLint
 * setup does not cover this folder, so this test is the enforcement. It strips comments and
 * string literals, then looks for syntax and built-ins that ES5 runtimes lack.
 */

var test = require('node:test');
var assert = require('node:assert/strict');
var fs = require('node:fs');
var path = require('node:path');

var LIB = path.join(__dirname, '..', 'lib');

function stripCommentsAndStrings(src) {
    var out = '';
    var i = 0;
    var c, n, quote;
    while (i < src.length) {
        c = src.charAt(i);
        n = src.charAt(i + 1);
        if (c === '/' && n === '/') {
            while (i < src.length && src.charAt(i) !== '\n') { i++; }
        } else if (c === '/' && n === '*') {
            i += 2;
            while (i < src.length && !(src.charAt(i) === '*' && src.charAt(i + 1) === '/')) { out += src.charAt(i) === '\n' ? '\n' : ''; i++; }
            i += 2;
        } else if (c === '\'' || c === '"') {
            quote = c;
            i++;
            while (i < src.length && src.charAt(i) !== quote) { i += src.charAt(i) === '\\' ? 2 : 1; }
            i++;
            out += '""';
        } else if (c === '`') {
            out += '`';
            i++;
        } else {
            out += c;
            i++;
        }
    }
    return out;
}

var FORBIDDEN = [
    [/=>/, 'arrow function'],
    [/\b(let|const)\s/, 'let or const'],
    [/`/, 'template literal'],
    [/\bclass\s+\w/, 'class'],
    [/\.\.\.\w/, 'spread or rest'],
    [/\b(async|await)\b/, 'async or await'],
    [/\bPromise\b/, 'Promise'],
    [/\b(Map|Set|Symbol)\s*\(/, 'Map, Set or Symbol'],
    [/\.(includes|startsWith|endsWith|padStart|padEnd|repeat|flat|flatMap|findIndex|find)\(/, 'ES2015+ method'],
    [/\bObject\.(assign|values|entries|fromEntries)\b/, 'ES2015+ Object function'],
    [/\bArray\.from\b/, 'Array.from']
];

fs.readdirSync(LIB).filter(function (f) { return /\.js$/.test(f); }).forEach(function (file) {
    test('lib/' + file + ' uses only ES5', function () {
        var code = stripCommentsAndStrings(fs.readFileSync(path.join(LIB, file), 'utf8'));
        var problems = [];
        FORBIDDEN.forEach(function (rule) {
            if (rule[0].test(code)) { problems.push(rule[1]); }
        });
        assert.deepEqual(problems, []);
    });
});
