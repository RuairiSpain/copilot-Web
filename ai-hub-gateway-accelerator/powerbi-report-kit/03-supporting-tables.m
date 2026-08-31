// Two small reference tables the new measures depend on. Both are static
// example data — replace with a real source (a small Excel/SharePoint
// list, a SQL table, whatever your org already uses to track budgets and
// department membership) once you've confirmed the shape works.
//
// HOW TO ADD: Power BI Desktop -> Home -> Transform Data -> New Source ->
// Blank Query -> right-click the new query -> Advanced Editor -> paste one
// of the two scripts below (replacing the placeholder content) -> Done ->
// rename the query to match the table name noted in each header.

// ============================================================
// Table name: Budgets
// One row per cost center per month. Used by the "% of budget consumed"
// and budget-risk measures on the Org and Cost-Center pages.
// ============================================================
let
    Source = #table(
        {"costCenter", "month", "monthlyBudget", "currency"},
        {
            {"Finance",     #date(2026, 1, 1), 15000, "USD"},
            {"Finance",     #date(2026, 2, 1), 15000, "USD"},
            {"Engineering", #date(2026, 1, 1), 40000, "USD"},
            {"Engineering", #date(2026, 2, 1), 40000, "USD"},
            {"Sales",       #date(2026, 1, 1), 8000,  "USD"},
            {"Sales",       #date(2026, 2, 1), 8000,  "USD"}
        }
    ),
    #"Changed Type" = Table.TransformColumnTypes(
        Source,
        {{"costCenter", type text}, {"month", type date}, {"monthlyBudget", type number}, {"currency", type text}}
    )
in
    #"Changed Type"

// ============================================================
// Table name: AuthorizedCostCenters
// Maps a budget holder's UPN to the cost center(s) they're allowed to see.
// One row per (UPN, costCenter) pair — a budget holder responsible for
// more than one cost center gets more than one row. Used by the
// BudgetHolder RLS role in 02-rls-roles.dax.
// ============================================================
let
    Source = #table(
        {"upn", "costCenter"},
        {
            {"alice@contoso.com", "Finance"},
            {"bob@contoso.com",   "Engineering"},
            {"bob@contoso.com",   "Sales"}
        }
    ),
    #"Changed Type" = Table.TransformColumnTypes(Source, {{"upn", type text}, {"costCenter", type text}})
in
    #"Changed Type"
