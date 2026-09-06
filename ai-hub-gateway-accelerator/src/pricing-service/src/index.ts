// Entry point for the Functions v4 programming model — importing each
// function file registers it via `app.http(...)` / `app.timer(...)` as a
// side effect. Referenced by "main" in package.json.
import './functions/enrichPricing';
import './functions/refreshPricingCache';
