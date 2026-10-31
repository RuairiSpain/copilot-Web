'use strict';

var util = require('./util');

var issue = util.issue;
var lower = util.lower;

/*
 * Works out where every resource should be, from desired state alone.
 *
 * Rules applied here: R1 (an org falls back to its UNALLOCATED cost center),
 * R2 (a user in at most one funding team), R3's precedence (a direct assignment
 * beats a funding team) and R4 (repos are placed only by the cost_center property).
 *
 * Returns { assignments, userCenter, errors, warnings } where each assignment is
 * { type, name, costCenter, source } and userCenter maps a lower-case login to
 * the cost center name it resolves to, or null.
 */
function resolve(desired, policy) {
    var errors = [];
    var warnings = [];
    var assignments = [];
    var userCenter = util.dict();
    var teams = desired.fundingTeams || [];
    var members = desired.teamMembers || {};
    var users = desired.users || [];
    var centerNames = util.dict();
    var teamsOf = util.dict();
    var direct = util.dict();
    var logins = util.dict();
    var i, j, t, login, list, centers, u, c, repo, value;

    for (i = 0; i < (desired.costCenters || []).length; i++) {
        c = desired.costCenters[i];
        if (util.stateOf(c) !== 'draft') {
            centerNames[lower(c.name)] = c.name;
        }
    }

    for (i = 0; i < teams.length; i++) {
        t = teams[i];
        list = util.own(members, t.slug) || [];
        for (j = 0; j < list.length; j++) {
            login = lower(list[j]);
            teamsOf[login] = teamsOf[login] || [];
            teamsOf[login].push({ slug: t.slug, costCenter: t.costCenter });
            logins[login] = true;
        }
    }

    for (i = 0; i < users.length; i++) {
        u = users[i];
        logins[lower(u.login)] = true;
        if (u.directCostCenter) {
            direct[lower(u.login)] = u;
        }
    }

    /* R2: a user in two funding teams. */
    for (login in teamsOf) {
        if (Object.prototype.hasOwnProperty.call(teamsOf, login) && teamsOf[login].length > 1) {
            centers = util.dict();
            for (j = 0; j < teamsOf[login].length; j++) {
                centers[lower(teamsOf[login][j].costCenter)] = true;
            }
            if (Object.keys(centers).length > 1) {
                errors.push(issue('user_in_teams_of_different_cost_centers',
                    login + ' is in funding teams that belong to different cost centers (' + teamsOf[login].map(function (x) { return x.slug; }).join(', ') + ')', 'User:' + login));
            } else {
                warnings.push(issue('user_in_multiple_teams_same_center',
                    login + ' is in more than one funding team (' + teamsOf[login].map(function (x) { return x.slug; }).join(', ') + ')', 'User:' + login));
            }
        }
    }

    /* Which cost center each user resolves to: direct beats team; a team conflict resolves to nothing. */
    for (login in logins) {
        if (Object.prototype.hasOwnProperty.call(logins, login)) {
            userCenter[login] = null;
            if (teamsOf[login] && teamsOf[login].length) {
                centers = util.dict();
                for (j = 0; j < teamsOf[login].length; j++) {
                    centers[lower(teamsOf[login][j].costCenter)] = teamsOf[login][j].costCenter;
                }
                if (Object.keys(centers).length === 1) {
                    userCenter[login] = teamsOf[login][0].costCenter;
                }
            }
            if (direct[login]) {
                if (userCenter[login] && lower(userCenter[login]) !== lower(direct[login].directCostCenter)) {
                    warnings.push(issue('direct_assignment_overrides_team',
                        login + ' has a direct assignment to ' + direct[login].directCostCenter + ' that overrides funding team placement in ' + userCenter[login], 'User:' + login));
                }
                userCenter[login] = direct[login].directCostCenter;
            }
        }
    }

    /* R1: each active org falls back to its UNALLOCATED cost center. */
    (desired.orgs || []).forEach(function (org) {
        if (org.state === undefined || org.state === 'active') {
            assignments.push({ type: 'Org', name: org.login, costCenter: org.fallbackCostCenter || ('UNALLOCATED-' + org.login), source: 'fallback' });
        }
    });

    /* Funding teams: as team resources when GitHub accepts them, otherwise expanded to their members. */
    for (i = 0; i < teams.length; i++) {
        t = teams[i];
        if (policy.teamsSupported) {
            assignments.push({ type: 'Team', name: t.slug, costCenter: t.costCenter, source: 'team' });
        } else {
            list = util.own(members, t.slug) || [];
            for (j = 0; j < list.length; j++) {
                login = lower(list[j]);
                if (!direct[login]) {
                    assignments.push({ type: 'User', name: list[j], costCenter: t.costCenter, source: 'team' });
                }
            }
        }
    }

    for (login in direct) {
        if (Object.prototype.hasOwnProperty.call(direct, login)) {
            assignments.push({ type: 'User', name: direct[login].login, costCenter: direct[login].directCostCenter, source: 'direct' });
        }
    }

    /* R4: repos are placed only by the property. No property, or a value that matches no cost center, leaves the repo on its org's fallback. */
    (desired.repos || []).forEach(function (r) {
        repo = r.org + '/' + r.repo;
        value = r.costCenterValue;
        if (!value) {
            warnings.push(issue('repo_without_property', repo + ' has no cost_center property and stays on its org fallback', 'Repo:' + repo));
        } else if (!centerNames[lower(value)]) {
            warnings.push(issue('repo_invalid_property', repo + ' has cost_center "' + value + '", which matches no cost center', 'Repo:' + repo));
        } else {
            assignments.push({ type: 'Repo', name: repo, costCenter: centerNames[lower(value)], source: 'property' });
        }
    });

    /* Deterministic order. */
    assignments = util.sortBy(assignments, function (a) { return lower(a.costCenter) + '|' + a.type + '|' + lower(a.name); });
    return { assignments: assignments, userCenter: userCenter, errors: errors, warnings: warnings };
}

module.exports = { resolve: resolve };
