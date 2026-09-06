# Quota Override canvas app — build guide

The two buttons: a "Request More Budget" screen for any authenticated
employee, and an "Approve / Deny Requests" screen for `Quota.Approve`
role holders, both meant to be embedded directly into Power BI report
pages via the **Power Apps visual**.

## HONESTY NOTE — read this first

**This is Power Fx source, not a working `.msapp` file.** No Power Apps
Studio was available in this session, the same constraint that shaped
this fork's Power BI report kit (DAX/M + a build guide, never a real
`.pbix`). `RequestMoreBudget.fx.txt` and `ApproveDenyRequests.fx.txt` are
real, carefully-written formulas — reviewed for Power Fx syntax
correctness, consistent with how the connector's operations and schemas
are actually shaped — but **not executed**. Build the screens in Studio
yourself following the steps below, and verify each formula compiles and
behaves as expected there before trusting this with real approvals.

## Prerequisites

1. The authorization-gap fix is deployed: `Quota.Approve` Entra role +
   the Quota Override API in APIM (`bicep/infra/modules/quota-service/`).
2. The custom connector is imported — see `../README.md`. In Power Apps
   Studio, it should show up in the Data panel as **QuotaConnector**
   once you add it as a data source to your app.
3. You (or whoever tests the Approve/Deny screen) have the
   `Quota.Approve` role assigned in Entra ID.

## Build steps

1. **Power Apps Studio → Create → Blank canvas app** (Tablet format
   works well for embedding at typical report-page width).
2. **Data → Add data source → QuotaConnector** — sign in when prompted
   (this is the delegated OAuth2 flow from `apiProperties.json`; you're
   authenticating as yourself, not a shared service account, which is
   the whole point — see `../README.md`).
3. **Add a second screen** so the app has both `scrRequestBudget` and
   `scrApproveDeny` (rename the default screen to the first; Insert →
   New screen → Blank for the second).
4. For **each screen**, add the controls named in that screen's `.fx.txt`
   file (matching control type — Label, Text input, Button, Gallery —
   exactly as noted next to each control's name), then for each property
   listed, select the control, click the property dropdown above the
   canvas (defaults to `Text` or `OnSelect`), pick the property named in
   the file, and paste the formula from the file into the formula bar.
5. **Gallery template** (`ApproveDenyRequests.fx.txt`'s `galPending`):
   add the label/button controls *inside* the gallery's template area,
   not the screen — Power Apps Studio only lets `ThisItem` resolve
   correctly inside a gallery's own template.
6. **Save and publish** (File → Save, then Publish this app).

## Embedding in Power BI

1. In your Power BI report, **Insert → More visuals → get more
   visuals** (or Insert → Power Apps if it's already available), add
   the **Power Apps visual** to the report canvas.
2. In the visual's setup pane, **choose "Use an existing app"** and
   select the app you just published.
3. **Add fields to the visual's data field well** — this is what
   `PowerBIIntegration.Data` reads inside the app (see
   `RequestMoreBudget.fx.txt`'s `OnVisible`). For the Request Budget
   screen, add fields named (or aliased to) `scopeType`, `scopeId`,
   `subscriptionId`, `currentQuota` from whatever visual/table on the
   report page currently shows the viewer's own quota — the existing
   My Usage / cost-center pages already have these values, this just
   passes them into the embedded app instead of the viewer having to
   retype them.
4. **Full mechanics of the embedding step itself** (resizing, refresh
   behavior, edit-vs-view mode) are Microsoft's own, well-documented UI —
   see [Embed a Power Apps visual in a Power BI report](https://learn.microsoft.com/en-us/power-bi/visuals/power-bi-visualization-powerapp)
   rather than this guide re-describing screens that change over time.
5. Repeat for the Approve/Deny screen on your Budget Holder page — it
   doesn't read `PowerBIIntegration.Data` at all (it lists everything
   the signed-in approver can see, per the scope note above), so no
   field-well mapping is needed for it, just the same "embed the Power
   Apps visual, pick this app" steps.

## What to verify yourself before trusting this

- Submit a real test request against a non-production access contract
  and confirm it lands in `quota-override-requests` with the right
  `requestedBy` (should be your own `oid`, not anything you typed).
- Approve it and confirm `quota-overrides` gets the new document, and
  that `getQuotaAllowance` (via the APIM policy fragment) picks it up
  within the 300s cache window.
- Try approving with an account that does **not** have `Quota.Approve` —
  confirm you get a 401, not a silent success.
