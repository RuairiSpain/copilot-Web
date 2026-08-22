import { existsSync } from "node:fs";
import { defineConfig, devices } from "@playwright/test";

// This sandbox ships a pre-installed Chromium outside Playwright's usual
// browser cache (the harness explicitly says not to run `playwright
// install` here). Point at it only when it's actually present, so this
// config stays portable to a real dev machine or CI, where a normal
// `playwright install chromium` step puts the browser where Playwright
// expects it and this override is simply unused (undefined = default
// resolution).
const sandboxChromium = "/opt/pw-browsers/chromium";
const executablePath = existsSync(sandboxChromium) ? sandboxChromium : undefined;

export default defineConfig({
    testDir: "./e2e",
    timeout: 30_000,
    // Every spec shares one Postgres-backed test user and its session list
    // (see e2e/global-setup.ts) — running specs in parallel would race on
    // that shared state, so this suite stays serial for now.
    fullyParallel: false,
    workers: 1,
    retries: process.env.CI ? 1 : 0,
    reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
    globalSetup: "./e2e/global-setup.ts",
    use: {
        baseURL: "http://localhost:3000",
        storageState: "./e2e/.auth/storageState.json",
        trace: "retain-on-failure",
    },
    projects: [
        {
            name: "chromium",
            use: { ...devices["Desktop Chrome"], launchOptions: { executablePath } },
        },
    ],
    // Dev mode (not a production build) is fine here — the WS route,
    // session-manager, and every real API route this suite exercises
    // behave identically in dev; only Next's own page compilation is
    // slower on first hit, which `timeout` below already accounts for.
    webServer: {
        command: "npm run dev",
        url: "http://localhost:3000",
        reuseExistingServer: !process.env.CI,
        timeout: 60_000,
    },
});
