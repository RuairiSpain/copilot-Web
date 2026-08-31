// Rapid-succession-prompt detection. Custom columns on the EXISTING
// llm-usage-container query, same place as prior optional columns.
//
// IMPORTANT — read before building the page this feeds
// (04-report-build-guide.md, "Prompting Patterns"): a short gap between
// one user's requests is AMBIGUOUS. It can mean inefficient
// trial-and-error prompting — but it equally well describes a normal
// multi-turn conversation, an agent/tool-calling loop making several
// quick follow-up calls, or a batch script. This column flags a pattern
// worth a human looking into, not a verdict on anyone's prompting skill.
// Treat the resulting report as a prompt to ask "what's going on here",
// not as a performance metric — see the note in
// 12-prompt-chain-efficiency-measures.dax on who should see this data.

// ============================================================
// Requires the table sorted for the group-shift below. Applied as ONE
// step replacing the existing llm-usage-container query's last step —
// paste into Advanced Editor (Home -> Transform Data -> right-click
// llm-usage-container -> Advanced Editor), inserting this logic before
// the final "in" line, OR add as a separate step referencing the prior
// step's output (call it PriorStep below — rename to match your query's
// actual last step name).
// ============================================================

let
    Sorted = Table.Sort(PriorStep, {{"appId", Order.Ascending}, {"timestamp", Order.Ascending}}),
    GroupedByUser = Table.Group(
        Sorted,
        {"appId"},
        {
            {
                "RowsWithGap",
                each
                    let
                        grp = _,
                        timestamps = grp[timestamp],
                        // Shift the timestamp list by one so row N sees row N-1's
                        // timestamp as "previousTimestamp" — the standard M
                        // pattern for a "previous row within group" calculation.
                        previousTimestamps = List.InsertRange(List.RemoveLastN(timestamps, 1), 0, {null}),
                        withPrev = Table.FromColumns(
                            Table.ToColumns(grp) & {previousTimestamps},
                            Table.ColumnNames(grp) & {"previousTimestamp"}
                        )
                    in
                        withPrev,
                type table
            }
        }
    ),
    Combined = Table.Combine(GroupedByUser[RowsWithGap]),
    AddSecondsGap = Table.AddColumn(
        Combined,
        "secondsSincePreviousRequest",
        each if [previousTimestamp] = null then null else Duration.TotalSeconds([timestamp] - [previousTimestamp]),
        type nullable number
    ),
    AddRapidFireFlag = Table.AddColumn(
        AddSecondsGap,
        "isRapidFire",
        each [secondsSincePreviousRequest] <> null and [secondsSincePreviousRequest] < 10,
        type logical
    )
    // 10-second threshold — adjustable. Chosen as "faster than a human
    // could plausibly have read the previous response and composed a
    // considered follow-up", not a scientifically derived number.
in
    AddRapidFireFlag
