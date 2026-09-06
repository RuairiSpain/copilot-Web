// Entry point for the Functions v4 programming model — importing each
// function file registers it via `app.http(...)` / `app.timer(...)` as a
// side effect. Referenced by "main" in package.json. Same pattern as
// src/pricing-service/src/index.ts.
import './functions/getQuotaAllowance';
import './functions/submitQuotaRequest';
import './functions/decideQuotaRequest';
import './functions/listPendingQuotaRequests';
import './functions/markQuotaRequestNotified';
import './functions/expireQuotaOverrides';
import './functions/resetMonthlyQuotaOverrides';
import './functions/sendQuotaNotificationEmail';
import './functions/listRecentlyDecidedQuotaRequests';
import './functions/markRequesterNotified';
