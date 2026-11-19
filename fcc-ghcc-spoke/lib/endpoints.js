'use strict';

/*
 * The only file that knows GitHub URL shapes. Phase 0 spikes confirm or
 * correct each entry; keep the CONFIDENCE table in step with what was tested.
 */

var API_VERSION = '2022-11-28';

function billing(enterprise) {
    return '/enterprises/' + encodeURIComponent(enterprise) + '/settings/billing';
}

var paths = {
    costCenters: function (e) { return billing(e) + '/cost-centers'; },
    costCenter: function (e, id) { return billing(e) + '/cost-centers/' + encodeURIComponent(id); },
    costCenterResource: function (e, id) { return billing(e) + '/cost-centers/' + encodeURIComponent(id) + '/resource'; },
    budgets: function (e) { return billing(e) + '/budgets'; },
    budget: function (e, id) { return billing(e) + '/budgets/' + encodeURIComponent(id); },
    budgetUserStates: function (e, id) { return billing(e) + '/budgets/' + encodeURIComponent(id) + '/user-states'; }
};

/* Response keys that wrap list and single-object bodies. */
var keys = {
    costCenterList: 'costCenters',
    budgetList: 'budgets',
    budget: 'budget'
};

var CONFIDENCE = {
    'cost center list/create/get/update/delete paths': 'documented (base path seen in GitHub docs search results)',
    'budget list/create/get/update/delete paths': 'documented (base path seen in GitHub docs search results)',
    'cost center resource path (POST/DELETE .../resource)': 'ASSUMED, confirm in Phase 0',
    'budget user-states path': 'ASSUMED, confirm in Phase 0',
    'response wrapper keys (costCenters, budgets, budget)': 'ASSUMED, confirm in Phase 0',
    'pagination (has_next_page body flag, page and per_page query)': 'ASSUMED, confirm in Phase 0',
    'request field names for budgets': 'ASSUMED from org-level billing docs, confirm in Phase 0',
    'user field on user budgets': 'required per PRD, absent from docs schema'
};

module.exports = {
    API_VERSION: API_VERSION,
    paths: paths,
    keys: keys,
    CONFIDENCE: CONFIDENCE
};
