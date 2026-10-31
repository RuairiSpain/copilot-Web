'use strict';

var util = require('./util');
var budgetmap = require('./budgetmap');
var resolveMod = require('./resolve');
var planMod = require('./plan');
var reconcileMod = require('./reconcile');

var lower = util.lower;
var key = util.key;

var CHECKS = {
    LICENSED_USER_UNALLOCATED: 'licensed_user_unallocated',
    USER_IN_TEAMS_OF_TWO_COST_CENTERS: 'user_in_teams_of_two_cost_centers',
    COST_CENTER_BUDGET: 'cost_center_budget',
    OVERLAPPING_BUDGETS: 'overlapping_budgets',
    USER_BUDGET_ZERO: 'user_budget_zero',
    REPO_PROPERTY: 'repo_property',
    SPEND_SPIKE: 'spend_spike',
    BUDGET_THRESHOLD: 'budget_threshold',
    POOL_THRESHOLD: 'pool_threshold',
    EXPIRED_OVERRIDE_ACTIVE: 'expired_override_active'
};

var SEVERITY_RANK = { high: 0, medium: 1, alert: 2, low: 3 };

var DEFAULT_SPIKE = {
    multiplier: 3,        /* today must be at least this many times the baseline */
    baselineDays: 28,     /* days before the latest day that make up the baseline */
    minHistoryDays: 7,    /* no spike is raised until this many days of history exist */
    minUsd: 25,           /* ignore small amounts */
    floorUsd: 5,          /* a baseline below this counts as this, so with no history a day needs at least multiplier x this, and minUsd */
    severity: 'medium'    /* PRD 4.6 leaves the severity configurable */
};

function addDays(iso, delta) {
    var p = iso.split('-');
    return new Date(Date.UTC(parseInt(p[0], 10), parseInt(p[1], 10) - 1, parseInt(p[2], 10) + delta)).toISOString().slice(0, 10);
}

function fixed(n) {
    return (Math.round(n * 100) / 100).toFixed(2);
}

function withDefaults(options) {
    var o = options || {};
    var spike = {};
    var k;
    for (k in DEFAULT_SPIKE) {
        if (Object.prototype.hasOwnProperty.call(DEFAULT_SPIKE, k)) {
            spike[k] = o.spike && o.spike[k] !== undefined ? o.spike[k] : DEFAULT_SPIKE[k];
        }
    }
    return {
        policy: planMod.withDefaults(o.policy),
        today: o.today,
        thresholds: (o.thresholds || [75, 90, 100]).slice().sort(function (a, b) { return a - b; }),
        spike: spike
    };
}

/*
 * Looks for misconfigurations and unusual spend (PRD 4.6). Pure: no clock, no network.
 *
 *   desired   as for plan()
 *   observed  as for plan(); observed.repoProperties is optional
 *   usage     { daily: [{ date, costCenter, usd }], userDaily: [{ date, login, usd }], pools: { costCenterName: limitUsd } }
 *             every part is optional; checks that need missing data are skipped
 *   options   { policy, today (YYYY-MM-DD, required), thresholds: [75, 90, 100],
 *               spike: { multiplier, baselineDays, minHistoryDays, minUsd, floorUsd, severity } }
 *
 * Each item: { key, check, subject, severity, message, details }. `key` is stable, so the same
 * condition always gets the same key and reconcileAnomalies() can avoid duplicates. Items are
 * ordered by severity, then check, then subject.
 */
function detectAnomalies(desired, observed, usage, optionsIn) {
    var o = withDefaults(optionsIn);
    var u = usage || {};
    var today = o.today;
    var centers = planMod.indexCenters(desired, observed);
    var resolution = resolveMod.resolve(desired, o.policy);
    var scopeMap = o.policy.budgetScopeMap || budgetmap.DEFAULT_SCOPE_MAP;
    var items = [];
    var location = {};
    var budgetsByIdentity = {};
    var centerBudget = {};
    var knownValues = {};

    if (!today) {
        throw new Error('options.today is required (YYYY-MM-DD)');
    }

    function add(check, subject, severity, message, discriminator, details) {
        items.push({
            key: check + ':' + subject + ':' + util.hash(String(discriminator)),
            check: check,
            subject: subject,
            severity: severity,
            message: message,
            details: details || {}
        });
    }

    Object.keys(centers.observedActive).forEach(function (n) {
        (centers.observedActive[n].resources || []).forEach(function (r) {
            location[key(r.type, r.name)] = centers.observedActive[n].name;
        });
        knownValues[n] = centers.observedActive[n].name;
    });
    Object.keys(centers.approved).forEach(function (n) { knownValues[n] = centers.approved[n].name; });

    (observed.budgets || []).forEach(function (b) {
        var id = budgetmap.identityOfObserved(b);
        budgetsByIdentity[id] = budgetsByIdentity[id] || [];
        budgetsByIdentity[id].push(b);
        if (b.budget_scope === scopeMap.cost_center && !centerBudget[lower(b.budget_entity_name)]) {
            centerBudget[lower(b.budget_entity_name)] = b;
        }
    });

    /* Where a user really is in GitHub: a direct resource, else a team they belong to, else their org's placement. */
    function observedCenterOf(user) {
        var login = user.login;
        var slugs, i, found;
        if (location[key('User', login)]) { return location[key('User', login)]; }
        slugs = Object.keys(desired.teamMembers || {}).sort();
        for (i = 0; i < slugs.length; i++) {
            if (desired.teamMembers[slugs[i]].some(function (m) { return lower(m) === lower(login); }) && location[key('Team', slugs[i])]) {
                found = location[key('Team', slugs[i])];
                break;
            }
        }
        if (found) { return found; }
        if (user.org && location[key('Org', user.org)]) { return location[key('Org', user.org)]; }
        return null;
    }

    /* Licensed users who are in a fallback cost center, or in none. */
    (desired.users || []).forEach(function (user) {
        var center;
        if (!user.licensed) { return; }
        center = observedCenterOf(user);
        if (!center) {
            add(CHECKS.LICENSED_USER_UNALLOCATED, 'User:' + user.login, 'high', 'Licensed user ' + user.login + ' is in no cost center', 'none', { center: null });
        } else if (/^unallocated-/.test(lower(center))) {
            add(CHECKS.LICENSED_USER_UNALLOCATED, 'User:' + user.login, 'high', 'Licensed user ' + user.login + ' is in the fallback cost center ' + center, center, { center: center });
        }
    });

    /* A user in funding teams of two cost centers. */
    resolution.errors.forEach(function (e) {
        if (e.code === 'user_in_teams_of_different_cost_centers') {
            add(CHECKS.USER_IN_TEAMS_OF_TWO_COST_CENTERS, e.subject, 'high', e.message, e.message, {});
        }
    });

    /* A cost center with no budget, or with stop usage off. */
    util.sortBy(Object.keys(centers.observedActive), function (n) { return n; }).forEach(function (n) {
        var name = centers.observedActive[n].name;
        var b = centerBudget[n];
        if (!b) {
            add(CHECKS.COST_CENTER_BUDGET, 'CostCenter:' + name, 'high', 'Cost center "' + name + '" has no budget', 'none', { reason: 'no_budget' });
        } else if (b.prevent_further_usage !== true) {
            add(CHECKS.COST_CENTER_BUDGET, 'CostCenter:' + name, 'high', 'The budget for "' + name + '" does not stop usage when it is reached', 'stop_off', { reason: 'stop_usage_off', budgetId: b.id });
        }
    });

    /* More than one budget for the same scope, entity and user. */
    Object.keys(budgetsByIdentity).sort().forEach(function (id) {
        var list = budgetsByIdentity[id];
        var first = list[0];
        var ids;
        if (list.length > 1) {
            ids = list.map(function (b) { return b.id; }).sort();
            add(CHECKS.OVERLAPPING_BUDGETS, 'Budget:' + first.budget_scope + ':' + (first.budget_entity_name || first.user || 'all'), 'medium',
                list.length + ' budgets overlap for the same target (' + ids.join(', ') + ')', ids.join(','), { budgetIds: ids });
        }
    });

    /* A per-user budget of $0. */
    (observed.budgets || []).forEach(function (b) {
        if ((b.budget_scope === 'user' || b.budget_scope === 'multi_user') && b.budget_amount === 0) {
            add(CHECKS.USER_BUDGET_ZERO, 'Budget:' + b.budget_scope + ':' + (b.user || b.budget_entity_name || 'all'), 'medium',
                'A user budget is set to $0, which blocks the user', b.id, { budgetId: b.id });
        }
    });

    /* Repos with no property or an invalid value, with a likely intended value when one is close. */
    (observed.repoProperties || []).forEach(function (r) {
        var best = null;
        var bestDistance = 3;
        var name, d, subject;
        if (r.value && knownValues[lower(r.value)]) { return; }
        subject = 'Repo:' + r.org + '/' + r.repo;
        for (name in knownValues) {
            if (r.value && Object.prototype.hasOwnProperty.call(knownValues, name)) {
                d = util.distance(lower(r.value), name);
                if (d < bestDistance) { bestDistance = d; best = knownValues[name]; }
            }
        }
        add(CHECKS.REPO_PROPERTY, subject, 'medium',
            r.value ? subject + ' has cost_center "' + r.value + '", which matches no cost center' + (best ? '; did you mean "' + best + '"?' : '') : subject + ' has no cost_center property',
            r.value || '', { value: r.value || null, proposed: best });
    });

    /* Overrides that should have expired but are still in GitHub. */
    (desired.budgets || []).forEach(function (b) {
        var mapped, id;
        if (b.scope !== 'override' || !b.expiry || b.expiry >= today) { return; }
        mapped = budgetmap.toGithub(b, o.policy.budgetScopeMap);
        id = budgetmap.identityOfMapped(mapped);
        if (budgetsByIdentity[id]) {
            add(CHECKS.EXPIRED_OVERRIDE_ACTIVE, 'Budget:override:' + b.user, 'medium',
                'The override for ' + b.user + ' expired on ' + b.expiry + ' but is still active', b.expiry, { expiry: b.expiry, budgetId: budgetsByIdentity[id][0].id });
        }
    });

    /* Spend against budget and pool, and spend spikes. */
    (function usageChecks() {
        var month = today.slice(0, 7);
        var spendByCenter = {};
        var latest = null;
        var allDates = [];
        var series = { center: {}, user: {} };

        function collect(rows, kind, field) {
            (rows || []).forEach(function (r) {
                var subject = lower(r[field]);
                if (r.date > today) { return; }
                series[kind][subject] = series[kind][subject] || { name: r[field], byDate: {} };
                series[kind][subject].byDate[r.date] = (series[kind][subject].byDate[r.date] || 0) + r.usd;
                allDates.push(r.date);
                if (kind === 'center' && r.date.slice(0, 7) === month) {
                    spendByCenter[subject] = (spendByCenter[subject] || 0) + r.usd;
                }
            });
        }
        collect(u.daily, 'center', 'costCenter');
        collect(u.userDaily, 'user', 'login');

        function thresholdLevel(percent) {
            var level = null;
            o.thresholds.forEach(function (t) { if (percent >= t) { level = t; } });
            return level;
        }

        Object.keys(spendByCenter).sort().forEach(function (n) {
            var name = series.center[n].name;
            var b = centerBudget[n];
            var pool = u.pools && u.pools[name];
            var spent = spendByCenter[n];
            var level;
            if (b && b.budget_amount > 0) {
                level = thresholdLevel(spent / b.budget_amount * 100);
                if (level !== null) {
                    add(CHECKS.BUDGET_THRESHOLD, 'CostCenter:' + name, 'alert',
                        '"' + name + '" is at ' + Math.floor(spent / b.budget_amount * 100) + '% of its $' + b.budget_amount + ' budget ($' + fixed(spent) + ' spent this month)',
                        month + ':' + level, { level: level, percent: Math.floor(spent / b.budget_amount * 100), spent: spent, budget: b.budget_amount, month: month });
                }
            }
            if (pool > 0) {
                level = thresholdLevel(spent / pool * 100);
                if (level !== null) {
                    add(CHECKS.POOL_THRESHOLD, 'CostCenter:' + name, 'alert',
                        '"' + name + '" is at ' + Math.floor(spent / pool * 100) + '% of its $' + pool + ' pool ($' + fixed(spent) + ' spent this month)',
                        month + ':' + level, { level: level, percent: Math.floor(spent / pool * 100), spent: spent, pool: pool, month: month });
                }
            }
        });

        /* Spikes: the latest day with data against the daily average of the days before it. */
        allDates.sort();
        if (!allDates.length) { return; }
        latest = allDates[allDates.length - 1];
        if (allDates[0] > addDays(latest, -o.spike.minHistoryDays)) { return; }

        ['center', 'user'].forEach(function (kind) {
            Object.keys(series[kind]).sort().forEach(function (n) {
                var s = series[kind][n];
                var current = s.byDate[latest] || 0;
                var sum = 0;
                var i, average, label;
                for (i = 1; i <= o.spike.baselineDays; i++) { sum += s.byDate[addDays(latest, -i)] || 0; }
                average = sum / o.spike.baselineDays;
                if (current >= o.spike.minUsd && current >= o.spike.multiplier * Math.max(average, o.spike.floorUsd)) {
                    label = kind === 'center' ? 'CostCenter:' : 'User:';
                    add(CHECKS.SPEND_SPIKE, label + s.name, o.spike.severity,
                        s.name + ' spent $' + fixed(current) + ' on ' + latest + ', ' + (Math.round(current / Math.max(average, o.spike.floorUsd) * 10) / 10) + 'x the ' + o.spike.baselineDays + '-day daily average of $' + fixed(average),
                        latest, { date: latest, spent: current, average: average, kind: kind });
                }
            });
        });
    }());

    return util.sortBy(items, function (i) {
        return (100 + SEVERITY_RANK[i.severity]) + '|' + i.check + '|' + i.subject + '|' + i.key;
    });
}

/*
 * Keeps the anomaly table free of duplicates across syncs. `existing` is [{ key, state }].
 * An open, dismissed or accepted anomaly is not raised again; one that was resolved can recur.
 */
function reconcileAnomalies(detected, existing) {
    return reconcileMod.reconcileRecords(detected, existing, ['open', 'dismissed', 'accepted'], ['open']);
}

module.exports = {
    detectAnomalies: detectAnomalies,
    reconcileAnomalies: reconcileAnomalies,
    CHECKS: CHECKS,
    DEFAULT_SPIKE: DEFAULT_SPIKE
};
