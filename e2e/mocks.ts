import type { Page } from "@playwright/test";
import type { RepoSummary } from "../src/types/session";

/** Stands in for `GET /api/repos` so the new-session dialog's repo dropdown
 * never needs a real GitHub API call or a real stored GitHub token. */
export async function mockRepoList(page: Page, repos: RepoSummary[]) {
    await page.route("**/api/repos", (route) => route.fulfill({ json: { repos } }));
}

interface FakeEvent {
    type: string;
    data: unknown;
}

/**
 * Replaces the real `/ws/sessions/:id` connection with a fully in-browser
 * fake. `page.routeWebSocket` never opens the real server-side connection
 * unless `connectToServer()` is called (see
 * https://playwright.dev/docs/mock#mock-websockets) — we don't call it, so
 * this exercises the whole app (login, repo picker, session creation, the
 * chat UI's event-to-message rendering, the composer) without needing a
 * live `@github/copilot-sdk` runtime or real Copilot access in CI.
 *
 * Every mocked connection starts with the same empty backlog a brand-new
 * session's real WebSocket connection sends (src/server/ws.ts's `backlog`
 * message on connect), then `script(promptText)` supplies the "backend"'s
 * reply events for each prompt the page sends.
 */
export async function mockSessionSocket(page: Page, script: (promptText: string) => FakeEvent[]) {
    await page.routeWebSocket(/\/ws\/sessions\//, (ws) => {
        let seq = 0;
        const send = (type: string, data: unknown) => {
            seq += 1;
            ws.send(JSON.stringify({ kind: "event", event: { seq, type, data, createdAt: new Date().toISOString() } }));
        };

        ws.send(JSON.stringify({ kind: "backlog", events: [], lastSeq: 0 }));

        ws.onMessage((raw) => {
            const message = JSON.parse(raw.toString()) as { kind: string; text?: string };
            if (message.kind !== "prompt" || !message.text) return;
            send("user.message", { content: message.text });
            for (const event of script(message.text)) send(event.type, event.data);
        });
    });
}
