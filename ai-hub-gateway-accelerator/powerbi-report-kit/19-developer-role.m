// Supporting table for the new Developer RLS role — same pattern as
// AuthorizedCostCenters in 03-supporting-tables.m, but mapping a
// developer's UPN to the app(s)/agent(s) they own, not a cost center.
//
// Table name: AuthorizedApps

let
    Source = #table(
        {"upn", "appId"},
        {
            {"carol@contoso.com", "support-triage-agent"},
            {"carol@contoso.com", "internal-docs-bot"},
            {"dave@contoso.com",  "sales-copilot"}
        }
    ),
    #"Changed Type" = Table.TransformColumnTypes(Source, {{"upn", type text}, {"appId", type text}})
in
    #"Changed Type"
