// Deprecated-model reference table — same pattern as Budgets and
// AuthorizedCostCenters in 03-supporting-tables.m: static example data,
// replace with your real source.
//
// I did not find a way to reliably pull live model-retirement dates into
// Power BI without an authenticated (service-principal) call to an Azure
// API whose exact shape I hadn't verified — rather than ship an unverified
// live connector, this is a manually maintained table. A Platform Admin
// updates it from Microsoft's published Azure OpenAI/Foundry model
// retirement notices. Automating the pull is a real future upgrade, not
// attempted here.
//
// Table name: ModelDeprecationSchedule

let
    Source = #table(
        {"model", "deprecationDate", "retirementDate", "replacementModel"},
        {
            {"gpt-4.1",   #date(2026, 10, 1), #date(2027, 1, 1), "gpt-5.2"},
            {"Phi-4",     #date(2026, 11, 1), #date(2027, 2, 1), "openai.gpt-oss-20b"}
        }
    ),
    #"Changed Type" = Table.TransformColumnTypes(
        Source,
        {{"model", type text}, {"deprecationDate", type date}, {"retirementDate", type date}, {"replacementModel", type text}}
    )
in
    #"Changed Type"
