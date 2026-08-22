import { test, expect } from "@playwright/test";
import { mockRepoList, mockSessionSocket } from "./mocks";
import { TEST_USER } from "./test-user";

/**
 * End-to-end smoke test for the vertical slice the build plan called for:
 * signed-in user → create a session against a repo → send a prompt → see
 * the assistant's reply render. Login and the session's WebSocket backend
 * are mocked (see global-setup.ts and mocks.ts); everything else —
 * routing, the session list/creation API, the DB-backed session row, the
 * chat UI's event-to-message reducer, the composer — is real.
 */
test.describe("chat flow", () => {
    const repo = {
        fullName: "e2e-test-user/demo-repo",
        private: false,
        defaultBranch: "main",
        description: "Fixture repo for the E2E suite",
        updatedAt: new Date().toISOString(),
    };

    test("sign in, create a session, send a message, see the assistant reply", async ({ page }) => {
        await mockRepoList(page, [repo]);
        await mockSessionSocket(page, (promptText) => [
            { type: "assistant.message_delta", data: { deltaContent: "Hello! " } },
            { type: "assistant.message_delta", data: { deltaContent: `You said: "${promptText}".` } },
            { type: "session.idle", data: {} },
        ]);

        await page.goto("/sessions");
        await expect(page.getByText(`Signed in as ${TEST_USER.login}`)).toBeVisible();
        await expect(page.getByText("No sessions yet")).toBeVisible();

        // In dev mode the page's client-side JS chunk can still be loading
        // after the SSR'd content above is already visible/interactable by
        // Playwright's actionability checks; a click that lands before
        // hydration finishes reaches a plain DOM button with no listener
        // attached yet. Retrying the click until the dialog it opens is
        // actually visible rides out that race instead of requiring an
        // arbitrary fixed delay.
        await expect(async () => {
            await page.getByRole("button", { name: "+ New" }).click();
            await expect(page.getByRole("heading", { name: "New session" })).toBeVisible({ timeout: 2000 });
        }).toPass({ timeout: 15_000 });

        await page.getByRole("combobox").selectOption(repo.fullName);
        // Interactive is already the dialog's default mode — nothing to
        // click in the mode selector for this smoke test.
        await page.getByRole("button", { name: "Start session" }).click();

        await expect(page).toHaveURL(/\/sessions\/[^/]+$/);
        // The session's title defaults to the repo's full name (see
        // POST /api/sessions), so it legitimately appears twice here: once
        // as the header's title, once in its "repo · mode" subline.
        await expect(page.getByText(repo.fullName).first()).toBeVisible();

        const composer = page.getByPlaceholder("Message Copilot…");
        await composer.click();
        await composer.fill("What does this repo do?");
        await composer.press("Enter");

        await expect(page.getByText("What does this repo do?", { exact: true })).toBeVisible();
        await expect(page.getByText('Hello! You said: "What does this repo do?".')).toBeVisible();

        // The session we just created also shows up back on the list —
        // proof the row is real (Prisma/Postgres), not just client state.
        // (Again matches twice — the session's title and its repo subline
        // both default to the same repo full name.)
        await page.goto("/sessions");
        await expect(page.getByText(repo.fullName).first()).toBeVisible();
    });
});
