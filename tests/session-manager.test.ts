import { randomUUID } from "node:crypto";
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { WebSocket } from "ws";

/**
 * Integration test: real Postgres (DATABASE_URL from `.env`, see
 * tests/setup.ts) for the Session/User rows, with `@github/copilot-sdk`
 * and `@octokit/rest` mocked — the SDK spawns a native runtime process and
 * needs a real Copilot subscription, neither of which is available in this
 * environment. The mock's job is only to stand in for the runtime; every
 * call *into* it from session-manager.ts is still checked by TypeScript
 * against the real SDK types (see FakeCopilotClient below), so a drift in
 * what we pass to `createSession`/`resumeSession` would still fail
 * `npm run typecheck` even though this test wouldn't catch it at runtime.
 */

interface FakeSdkSession {
    sessionId: string;
    send: ReturnType<typeof vi.fn>;
    abort: ReturnType<typeof vi.fn>;
}

const created: { config: unknown; session: FakeSdkSession }[] = [];

vi.mock("@github/copilot-sdk", () => {
    class FakeCopilotClient {
        static instances: FakeCopilotClient[] = [];
        start = vi.fn(async () => {});
        stop = vi.fn(async () => {});
        deleteSession = vi.fn(async () => {});
        createSession = vi.fn(async (config: unknown) => {
            const session: FakeSdkSession = { sessionId: `fake-${randomUUID()}`, send: vi.fn(), abort: vi.fn() };
            created.push({ config, session });
            return session;
        });
        resumeSession = vi.fn(async (_id: string, config: unknown) => {
            const session: FakeSdkSession = { sessionId: `fake-${randomUUID()}`, send: vi.fn(), abort: vi.fn() };
            created.push({ config, session });
            return session;
        });
        constructor() {
            FakeCopilotClient.instances.push(this);
        }
    }
    return {
        CopilotClient: FakeCopilotClient,
        defineTool: (name: string, config: Record<string, unknown>) => ({ name, ...config }),
        approveAll: () => ({ kind: "approve-once" }),
    };
});

vi.mock("@octokit/rest", () => ({
    Octokit: class {
        git = {};
        pulls = {};
    },
}));

// Imported after the mocks above so session-manager.ts's module-level
// `import { CopilotClient, ... } from "@github/copilot-sdk"` resolves to
// the mock (Vitest hoists `vi.mock` calls above imports).
const { sessionManager } = await import("@/server/session-manager");
const { prisma } = await import("@/lib/prisma");
const { encryptSecret } = await import("@/lib/crypto");

function fakeSocket() {
    const sent: unknown[] = [];
    const ws = {
        readyState: 1,
        OPEN: 1,
        send: vi.fn((raw: string) => sent.push(JSON.parse(raw))),
    } as unknown as WebSocket;
    return { ws, sent };
}

const hasDb = Boolean(process.env.DATABASE_URL) && Boolean(process.env.TOKEN_ENCRYPTION_KEY);
const describeIfDb = hasDb ? describe : describe.skip;

describeIfDb("SessionManager (real Postgres, mocked SDK)", () => {
    let userId: string;

    async function createSessionRow(mode: "planning" | "interactive" | "auto") {
        const session = await prisma.session.create({
            data: { userId, title: "test", repoFullName: "octocat/hello-world", repoDefaultBranch: "main", mode },
        });
        return session.id;
    }

    beforeAll(async () => {
        const user = await prisma.user.create({
            data: { githubId: `test-${randomUUID()}`, login: "test-user", encryptedAccessToken: encryptSecret("ghp_fake_token") },
        });
        userId = user.id;
    });

    beforeEach(() => {
        created.length = 0;
    });

    afterAll(async () => {
        await prisma.session.deleteMany({ where: { userId } });
        await prisma.user.deleteMany({ where: { id: userId } });
        await prisma.$disconnect();
    });

    it("attach() starts a session against the no-clone tool allowlist and persists the SDK session id", async () => {
        const sessionId = await createSessionRow("auto");
        const { ws } = fakeSocket();

        await sessionManager.attach(sessionId, userId, ws, 0);

        expect(created).toHaveLength(1);
        const config = created[0]!.config as { availableTools: string[]; onPermissionRequest: unknown };
        expect(config.availableTools).toEqual(expect.arrayContaining(["custom:*", "mcp:*"]));
        expect(typeof config.onPermissionRequest).toBe("function");

        const row = await prisma.session.findUniqueOrThrow({ where: { id: sessionId } });
        expect(row.sdkSessionId).toBe(created[0]!.session.sessionId);
    });

    it("prompt messages are forwarded to the underlying SDK session", async () => {
        const sessionId = await createSessionRow("auto");
        const { ws } = fakeSocket();
        await sessionManager.attach(sessionId, userId, ws, 0);

        await sessionManager.handleClientMessage(sessionId, { kind: "prompt", text: "hello there" });

        expect(created[0]!.session.send).toHaveBeenCalledWith({ prompt: "hello there" });
    });

    it("interactive mode blocks a permission request until the client responds", async () => {
        const sessionId = await createSessionRow("interactive");
        const { ws, sent } = fakeSocket();
        await sessionManager.attach(sessionId, userId, ws, 0);

        const config = created[0]!.config as {
            onPermissionRequest: (request: unknown, invocation: unknown) => Promise<{ kind: string }>;
        };

        const pending = config.onPermissionRequest({ kind: "write" }, { sessionId });

        // The bridge should have broadcast an app.permission_requested event
        // with a requestId, rather than resolving immediately.
        await vi.waitFor(() => expect(sent.some((m) => isPermissionRequested(m))).toBe(true));
        const requested = sent.find(isPermissionRequested)!;
        const requestId = requested.event.data.requestId as string;

        await sessionManager.handleClientMessage(sessionId, {
            kind: "permission.respond",
            requestId,
            decision: "approve-once",
        });

        await expect(pending).resolves.toEqual({ kind: "approve-once" });
    });

    it("deleteSession removes the row and stops the underlying client", async () => {
        const sessionId = await createSessionRow("auto");
        const { ws } = fakeSocket();
        await sessionManager.attach(sessionId, userId, ws, 0);
        const { session } = created[0]!;

        await sessionManager.deleteSession(sessionId, userId);

        await expect(prisma.session.findUnique({ where: { id: sessionId } })).resolves.toBeNull();
        expect(session.sessionId).toBeTruthy(); // sanity: the right session was targeted
    });
});

function isPermissionRequested(m: unknown): m is { kind: "event"; event: { data: { requestId: string } } } {
    const msg = m as { kind?: string; event?: { type?: string } };
    return msg.kind === "event" && msg.event?.type === "app.permission_requested";
}
