'use strict';

var test = require('node:test');
var assert = require('node:assert/strict');
var lib = require('../lib');
var f = require('./plan-fixtures');

/* GitHub as desired(), plus the budget the UNALLOCATED fallback also needs (PRD: every cost center has a budget). */
function clean() {
    var o = f.observedMatching();
    o.budgets.push({ id: 'b-2', budget_scope: 'cost_center', budget_entity_name: 'UNALLOCATED-acme-platform', budget_amount: 100, prevent_further_usage: true });
    return o;
}

function detect(observed, usage, options, desired) {
    var opts = { today: f.TODAY, policy: f.policy() };
    var k;
    for (k in options || {}) { if (Object.prototype.hasOwnProperty.call(options, k)) { opts[k] = options[k]; } }
    return lib.detectAnomalies(desired || f.desired(), observed, usage || {}, opts);
}

function checks(items) { return items.map(function (i) { return i.check; }); }
function one(items, check) { return items.filter(function (i) { return i.check === check; })[0]; }

function range(from, to) {
    var out = [];
    var d = new Date(from + 'T00:00:00Z');
    var end = new Date(to + 'T00:00:00Z');
    while (d <= end) { out.push(d.toISOString().slice(0, 10)); d = new Date(d.getTime() + 86400000); }
    return out;
}

test('a clean estate has no anomalies', function () {
    assert.deepEqual(detect(clean()), []);
});

test('today is required, because the engine never reads a clock', function () {
    assert.throws(function () { lib.detectAnomalies(f.desired(), clean(), {}, {}); }, /detectAnomalies: today is required/);
});

test('a cost center with no budget is high severity', function () {
    var o = clean();
    o.budgets = o.budgets.filter(function (b) { return b.budget_entity_name !== 'Platform'; });
    var items = detect(o);
    assert.deepEqual(checks(items), ['cost_center_budget']);
    assert.equal(items[0].severity, 'high');
    assert.equal(items[0].subject, 'CostCenter:Platform');
    assert.equal(items[0].details.reason, 'no_budget');
});

test('a cost center whose budget does not stop usage is high severity', function () {
    var o = clean();
    o.budgets[0].prevent_further_usage = false;
    var d = detect(o)[0];
    assert.equal(d.details.reason, 'stop_usage_off');
    assert.equal(d.severity, 'high');
});

test('the UNALLOCATED fallback needs a budget like any other cost center', function () {
    var o = f.observedMatching();
    var items = detect(o);
    assert.deepEqual(items.map(function (i) { return i.subject; }), ['CostCenter:UNALLOCATED-acme-platform']);
});

test('a licensed user in no cost center is high severity; an unlicensed one is ignored', function () {
    var d = f.desired();
    d.users.push({ login: 'nobody', licensed: true });
    d.users.push({ login: 'contractor', licensed: false });
    var items = detect(clean(), null, null, d);
    assert.deepEqual(items.map(function (i) { return i.subject; }), ['User:nobody']);
    assert.equal(items[0].severity, 'high');
    assert.match(items[0].message, /is in no cost center/);
});

test('a licensed user sitting in an UNALLOCATED cost center is high severity', function () {
    var o = clean();
    o.costCenters[0].resources.push({ type: 'User', name: 'parked' });
    var d = f.desired();
    d.users.push({ login: 'parked', licensed: true });
    var item = detect(o, null, null, d)[0];
    assert.equal(item.check, 'licensed_user_unallocated');
    assert.match(item.message, /fallback cost center UNALLOCATED-acme-platform/);
});

test('a licensed user with no placement of their own falls back to their org, which counts as UNALLOCATED', function () {
    var d = f.desired();
    d.users.push({ login: 'newbie', licensed: true, org: 'acme-platform' });
    var item = detect(clean(), null, null, d)[0];
    assert.equal(item.details.center, 'UNALLOCATED-acme-platform');
});

test('a user is placed through the team they belong to, as GitHub sees it', function () {
    var o = clean();
    o.costCenters[1].resources = o.costCenters[1].resources.filter(function (r) { return r.type !== 'Team'; });
    assert.deepEqual(detect(o).map(function (i) { return i.subject; }), ['User:ada', 'User:grace']);
});

test('a user in funding teams of two cost centers is high severity', function () {
    var d = f.desired();
    d.costCenters.push({ name: 'Data' });
    d.fundingTeams.push({ slug: 'data-sci', costCenter: 'Data' });
    d.teamMembers['data-sci'] = ['ada'];
    var o = clean();
    o.costCenters.push({ id: 'cc-3', name: 'Data', state: 'active', resources: [{ type: 'Team', name: 'data-sci' }] });
    o.budgets.push({ id: 'b-3', budget_scope: 'cost_center', budget_entity_name: 'Data', budget_amount: 10, prevent_further_usage: true });
    var item = one(detect(o, null, null, d), 'user_in_teams_of_two_cost_centers');
    assert.equal(item.severity, 'high');
    assert.equal(item.subject, 'User:ada');
});

test('two budgets for the same target are medium severity, with their ids', function () {
    var o = clean();
    o.budgets.push({ id: 'b-9', budget_scope: 'cost_center', budget_entity_name: 'platform', budget_amount: 5000, prevent_further_usage: true });
    var item = detect(o)[0];
    assert.equal(item.check, 'overlapping_budgets');
    assert.equal(item.severity, 'medium');
    assert.deepEqual(item.details.budgetIds, ['b-1', 'b-9']);
});

test('a per-user budget of $0 is medium severity', function () {
    var o = clean();
    o.budgets.push({ id: 'b-5', budget_scope: 'user', budget_entity_name: '', user: 'ada', budget_amount: 0, prevent_further_usage: true });
    o.budgets.push({ id: 'b-6', budget_scope: 'multi_user', budget_entity_name: 'Platform', budget_amount: 0, prevent_further_usage: true });
    o.budgets.push({ id: 'b-7', budget_scope: 'cost_center', budget_entity_name: 'Other', budget_amount: 0, prevent_further_usage: true });
    var zeros = detect(o).filter(function (i) { return i.check === 'user_budget_zero'; });
    assert.deepEqual(zeros.map(function (i) { return i.details.budgetId; }).sort(), ['b-5', 'b-6']);
    zeros.forEach(function (z) { assert.equal(z.severity, 'medium'); });
});

test('repos with no property or an invalid value are medium severity, and a likely fix is proposed', function () {
    var o = clean();
    o.repoProperties = [
        { org: 'acme-platform', repo: 'fine', value: 'platform' },
        { org: 'acme-platform', repo: 'bare', value: '' },
        { org: 'acme-platform', repo: 'typo', value: 'Platfrom' },
        { org: 'acme-platform', repo: 'wild', value: 'Marketing' }
    ];
    var items = detect(o).filter(function (i) { return i.check === 'repo_property'; });
    var by = {};
    items.forEach(function (i) { by[i.subject] = i; });
    assert.deepEqual(Object.keys(by).sort(), ['Repo:acme-platform/bare', 'Repo:acme-platform/typo', 'Repo:acme-platform/wild']);
    assert.equal(by['Repo:acme-platform/typo'].details.proposed, 'Platform');
    assert.match(by['Repo:acme-platform/typo'].message, /did you mean "Platform"/);
    assert.equal(by['Repo:acme-platform/wild'].details.proposed, null);
    assert.match(by['Repo:acme-platform/bare'].message, /no cost_center property/);
});

test('an override past its expiry that is still in GitHub is medium severity', function () {
    var d = f.desired();
    d.budgets.push({ scope: 'override', user: 'ada', amount: 100, preventUsage: true, expiry: '2026-10-28', override: true, costCenter: 'Platform' });
    var o = clean();
    o.budgets.push({ id: 'b-8', budget_scope: 'user', budget_entity_name: '', user: 'ada', budget_amount: 100, prevent_further_usage: true });
    var item = detect(o, null, null, d)[0];
    assert.equal(item.check, 'expired_override_active');
    assert.equal(item.severity, 'medium');
    assert.equal(item.details.budgetId, 'b-8');
    d.budgets[1].expiry = '2026-11-30';
    assert.deepEqual(detect(o, null, null, d), []);
    d.budgets[1].expiry = '2026-10-28';
    o.budgets.pop();
    assert.deepEqual(detect(o, null, null, d), []);
});

test('threshold alerts report only the highest level crossed, and nothing below 75%', function () {
    function alertFor(spend) {
        var usage = { daily: [{ date: '2026-10-15', costCenter: 'Platform', usd: spend }] };
        return one(detect(clean(), usage), 'budget_threshold');
    }
    assert.equal(alertFor(700), undefined);
    assert.equal(alertFor(750).details.level, 75);
    assert.equal(alertFor(899).details.level, 75);
    assert.equal(alertFor(900).details.level, 90);
    assert.equal(alertFor(1000).details.level, 100);
    assert.equal(alertFor(1400).details.level, 100);
    assert.equal(alertFor(910).severity, 'alert');
    assert.match(alertFor(910).message, /"Platform" is at 91% of its \$1000 budget \(\$910\.00 spent this month\)/);
});

test('spend counts only this month, and only up to today', function () {
    var usage = { daily: [
        { date: '2026-09-30', costCenter: 'Platform', usd: 5000 },
        { date: '2026-10-10', costCenter: 'Platform', usd: 400 },
        { date: '2026-10-20', costCenter: 'Platform', usd: 400 },
        { date: '2026-11-01', costCenter: 'Platform', usd: 9000 }
    ] };
    assert.equal(one(detect(clean(), usage), 'budget_threshold').details.spent, 800);
});

test('a new level in the same month is a new alert; the same level is the same alert', function () {
    function key(spend, today) {
        return one(detect(clean(), { daily: [{ date: today, costCenter: 'Platform', usd: spend }] }, { today: today }), 'budget_threshold').key;
    }
    assert.equal(key(760, '2026-10-15'), key(780, '2026-10-20'));
    assert.notEqual(key(760, '2026-10-15'), key(950, '2026-10-20'));
});

test('thresholds can be changed, and a pool is checked the same way', function () {
    var usage = { daily: [{ date: '2026-10-15', costCenter: 'Platform', usd: 600 }], pools: { Platform: 700 } };
    assert.equal(one(detect(clean(), usage), 'budget_threshold'), undefined);
    var pool = one(detect(clean(), usage), 'pool_threshold');
    assert.equal(pool.details.level, 75);
    assert.match(pool.message, /of its \$700 pool/);
    assert.equal(one(detect(clean(), usage, { thresholds: [50] }), 'budget_threshold').details.level, 50);
});

test('a spend spike is the latest day against the daily average of the 28 days before it', function () {
    var rows = range('2026-10-03', '2026-10-30').map(function (d) { return { date: d, login: 'dmurphy', usd: 10 }; });
    rows.push({ date: '2026-10-31', login: 'dmurphy', usd: 90 });
    var items = detect(clean(), { userDaily: rows });
    var spike = one(items, 'spend_spike');
    assert.equal(spike.subject, 'User:dmurphy');
    assert.equal(spike.severity, 'medium');
    assert.equal(spike.details.average, 10);
    assert.match(spike.message, /dmurphy spent \$90\.00 on 2026-10-31, 9x the 28-day daily average of \$10\.00/);
});

test('no spike for an ordinary day, a small amount, or too little history', function () {
    var base = range('2026-10-03', '2026-10-30').map(function (d) { return { date: d, login: 'dmurphy', usd: 10 }; });
    function run(today, rows) { return one(detect(clean(), { userDaily: rows }), 'spend_spike'); }
    assert.equal(run(0, base.concat([{ date: '2026-10-31', login: 'dmurphy', usd: 25 }])), undefined, 'under 3x');
    var quiet = range('2026-10-03', '2026-10-30').map(function (d) { return { date: d, login: 'q', usd: 0.5 }; });
    assert.equal(run(0, quiet.concat([{ date: '2026-10-31', login: 'q', usd: 20 }])), undefined, 'under the minimum amount');
    assert.equal(run(0, [{ date: '2026-10-29', login: 'new', usd: 1 }, { date: '2026-10-31', login: 'new', usd: 300 }]), undefined, 'not enough history');
});

test('spend spikes cover cost centers too, and the multiplier is configurable', function () {
    var rows = range('2026-10-03', '2026-10-30').map(function (d) { return { date: d, costCenter: 'Platform', usd: 40 }; });
    rows.push({ date: '2026-10-31', costCenter: 'Platform', usd: 130 });
    var spike = one(detect(clean(), { daily: rows }), 'spend_spike');
    assert.equal(spike.subject, 'CostCenter:Platform');
    assert.equal(spike.details.kind, 'center');
    assert.equal(one(detect(clean(), { daily: rows }, { spike: { multiplier: 4 } }), 'spend_spike'), undefined, '3.25x is under 4x');
});

test('a spike below the multiplier is not reported and one above it takes the configured severity', function () {
    var rows = range('2026-10-03', '2026-10-30').map(function (d) { return { date: d, costCenter: 'Platform', usd: 40 }; });
    rows.push({ date: '2026-10-31', costCenter: 'Platform', usd: 150 });
    var s = one(detect(clean(), { daily: rows }, { spike: { severity: 'high' } }), 'spend_spike');
    assert.equal(s.subject, 'CostCenter:Platform');
    assert.equal(s.severity, 'high');
});

test('items are ordered high, medium, then alert, and detection is deterministic', function () {
    var o = clean();
    o.budgets = o.budgets.filter(function (b) { return b.budget_entity_name !== 'Platform'; });
    o.budgets.push({ id: 'b-5', budget_scope: 'user', budget_entity_name: '', user: 'ada', budget_amount: 0, prevent_further_usage: true });
    var usage = { daily: [{ date: '2026-10-15', costCenter: 'UNALLOCATED-acme-platform', usd: 95 }] };
    var a = detect(o, usage);
    assert.deepEqual(a.map(function (i) { return i.severity; }), ['high', 'medium', 'alert']);
    assert.equal(JSON.stringify(a), JSON.stringify(detect(o, usage)));
    assert.equal(new Set(a.map(function (i) { return i.key; })).size, a.length);
});

test('checks that need usage or repo data are skipped when it is not supplied', function () {
    assert.deepEqual(detect(clean(), undefined), []);
    assert.deepEqual(detect(clean(), {}), []);
});

test('reconcile: open, dismissed and accepted anomalies are not raised again; resolved ones can recur', function () {
    var o = clean();
    o.budgets[0].prevent_further_usage = false;
    var d = detect(o);
    assert.equal(lib.reconcileAnomalies(d, []).create.length, 1);
    ['open', 'dismissed', 'accepted'].forEach(function (state) {
        var r = lib.reconcileAnomalies(d, [{ key: d[0].key, state: state }]);
        assert.equal(r.create.length, 0, state);
        assert.equal(r.unchanged.length, 1, state);
    });
    assert.equal(lib.reconcileAnomalies(d, [{ key: d[0].key, state: 'resolved' }]).create.length, 1);
});

test('reconcile: an open anomaly resolves when the condition goes, but a dismissed one is left alone', function () {
    var o = clean();
    o.budgets[0].prevent_further_usage = false;
    var d = detect(o);
    var fixed = detect(clean());
    assert.deepEqual(lib.reconcileAnomalies(fixed, [{ key: d[0].key, state: 'open' }]).resolve, [d[0].key]);
    assert.deepEqual(lib.reconcileAnomalies(fixed, [{ key: d[0].key, state: 'dismissed' }]).resolve, []);
    assert.deepEqual(lib.reconcileAnomalies(fixed, [{ key: d[0].key, state: 'accepted' }]).resolve, []);
});

test('usage dated later in the same month than today is ignored', function () {
    var usage = { daily: [
        { date: '2026-10-10', costCenter: 'Platform', usd: 400 },
        { date: '2026-10-25', costCenter: 'Platform', usd: 9000 }
    ] };
    assert.equal(one(detect(clean(), usage, { today: '2026-10-20' }), 'budget_threshold'), undefined);
    assert.equal(one(detect(clean(), usage, { today: '2026-10-25' }), 'budget_threshold').details.level, 100);
});
