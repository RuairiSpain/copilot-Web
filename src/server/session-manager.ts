import { randomUUID } from "node:crypto";
import type { WebSocket } from "ws";
import { z } from "zod";
import {
    CopilotClient,
    defineTool,
    type CopilotSession,
    type CustomAgentConfig,
    type MCPServerConfig,
    type SessionEvent as SdkSessionEvent,
} from "@github/copilot-sdk";
import type { SessionFunction } from "@prisma/client";
import { prisma } from "@/lib/prisma";
import { getGitHubTokenForUser } from "@/lib/octokit";
import { decryptSecret } from "@/lib/crypto";
import { GitHubFsProvider } from "./github-fs";
import { appendEvent, getEventsSince } from "./event-log";
import {
    createExitPlanModeHandler,
    createPermissionHandler,
    createPreToolUseGuard,
    createUserInputHandler,
    type PermissionBridge,
} from "./permission-modes";
import type { ClientSessionEvent, ClientToServerMessage, PermissionDecisionKind, SessionMode } from "@/types/session";

const MAX_LIVE_SESSIONS = Number(process.env.MAX_LIVE_SESSIONS ?? 8);
const IDLE_EVICTION_MS = 30 * 60 * 1000; // non-`auto` sessions only
const PERMISSION_TIMEOUT_MS = 15 * 60 * 1000;

/**
 * Built-in tool names available to every session running against the
 * GitHub-API virtual filesystem (see github-fs.ts). `CopilotClientMode:
 * "empty"` requires each session to declare `availableTools` explicitly
 * (see the SDK's src/types.ts CopilotClientOptions.mode doc). This list is
 * a best-effort mapping of the file/search tools referenced across the
 * SDK's docs and hook examples (`read`/`write`/`edit`/`create`/`glob`/
 * `grep`/`ls`) — verify it against the installed CLI's real tool catalog
 * (e.g. by inspecting `tool.execution_start` events from a live session)
 * and adjust. Deliberately excludes `bash`/`shell`: there is no working
 * directory for it to operate in without a real checkout.
 */
const NO_CLONE_AVAILABLE_TOOLS = ["read", "write", "edit", "create", "glob", "grep", "ls", "custom:*", "mcp:*"];

/** Virtual path (inside the GitHub-API filesystem, not real disk) where the
 * runtime stores its own session-state files. Excluded from
 * `commit_and_push` so runtime internals never end up committed to the
 * target repo. */
const SESSION_STATE_PATH = ".copilot-session-state";

interface PendingPermission {
    resolve: (decision: PermissionDecisionKind | "timeout") => void;
}
interface PendingPlan {
    resolve: (result: { approved: boolean; selectedAction?: string; feedback?: string } | "timeout") => void;
}
interface PendingUserInput {
    resolve: (result: { answer: string; wasFreeform: boolean } | "timeout") => void;
}

interface LiveSession {
    sessionId: string;
    mode: SessionMode;
    client: CopilotClient;
    sdkSession: CopilotSession;
    fsProvider: GitHubFsProvider;
    sockets: Set<WebSocket>;
    pendingPermissions: Map<string, PendingPermission>;
    pendingPlans: Map<string, PendingPlan>;
    pendingUserInputs: Map<string, PendingUserInput>;
    evictionTimer?: ReturnType<typeof setTimeout>;
}

class SessionManager {
    private live = new Map<string, LiveSession>();

    /** Attaches a WebSocket to a session, starting/resuming the underlying
     * CopilotClient/CopilotSession if it isn't already live, and returns the
     * persisted event backlog since `sinceSeq` for replay. */
    async attach(sessionId: string, userId: string, ws: WebSocket, sinceSeq: number) {
        const live = await this.ensureLive(sessionId, userId);
        live.sockets.add(ws);
        if (live.evictionTimer) {
            clearTimeout(live.evictionTimer);
            live.evictionTimer = undefined;
        }
        return getEventsSince(sessionId, sinceSeq);
    }

    detach(sessionId: string, ws: WebSocket) {
        const live = this.live.get(sessionId);
        if (!live) return;
        live.sockets.delete(ws);
        if (live.sockets.size === 0 && live.mode !== "auto") {
            // Interactive/planning sessions have nothing useful to do with
            // no one to ask, so free the runtime after a grace period. Auto
            // sessions are exempt by design — they're meant to keep working
            // unattended.
            live.evictionTimer = setTimeout(() => this.evict(sessionId), IDLE_EVICTION_MS);
        }
    }

    async handleClientMessage(sessionId: string, message: ClientToServerMessage) {
        const live = this.live.get(sessionId);
        if (!live) throw new Error(`Session ${sessionId} is not live`);

        switch (message.kind) {
            case "prompt":
                await live.sdkSession.send({ prompt: message.text });
                return;
            case "abort":
                await live.sdkSession.abort();
                return;
            case "permission.respond": {
                const pending = live.pendingPermissions.get(message.requestId);
                pending?.resolve(message.decision);
                live.pendingPermissions.delete(message.requestId);
                this.broadcastAppEvent(sessionId, "app.permission_resolved", {
                    requestId: message.requestId,
                    decision: message.decision,
                });
                return;
            }
            case "plan.respond": {
                const pending = live.pendingPlans.get(message.requestId);
                pending?.resolve({ approved: message.approved, selectedAction: message.selectedAction, feedback: message.feedback });
                live.pendingPlans.delete(message.requestId);
                this.broadcastAppEvent(sessionId, "app.plan_resolved", {
                    requestId: message.requestId,
                    approved: message.approved,
                    selectedAction: message.selectedAction,
                    feedback: message.feedback,
                });
                return;
            }
            case "ask_user.respond": {
                const pending = live.pendingUserInputs.get(message.requestId);
                pending?.resolve({ answer: message.answer, wasFreeform: message.wasFreeform });
                live.pendingUserInputs.delete(message.requestId);
                this.broadcastAppEvent(sessionId, "app.ask_user_resolved", {
                    requestId: message.requestId,
                    answer: message.answer,
                });
                return;
            }
        }
    }

    /** Used by the commit/PR tool (registered alongside each session) to
     * reach this session's virtual filesystem overlay. */
    getFsProvider(sessionId: string): GitHubFsProvider | undefined {
        return this.live.get(sessionId)?.fsProvider;
    }

    async deleteSession(sessionId: string, userId: string) {
        const row = await prisma.session.findUniqueOrThrow({ where: { id: sessionId } });
        if (row.userId !== userId) throw new Error("Not authorized to delete this session");

        const live = this.live.get(sessionId);
        if (live) {
            for (const p of live.pendingPermissions.values()) p.resolve("timeout");
            for (const p of live.pendingPlans.values()) p.resolve("timeout");
            for (const p of live.pendingUserInputs.values()) p.resolve("timeout");
            if (row.sdkSessionId) {
                await live.client.deleteSession(row.sdkSessionId).catch(() => undefined);
            }
            await live.client.stop().catch(() => undefined);
            this.live.delete(sessionId);
        }
        await prisma.session.delete({ where: { id: sessionId } });
    }

    private async ensureLive(sessionId: string, userId: string): Promise<LiveSession> {
        const existing = this.live.get(sessionId);
        if (existing) return existing;

        if (this.live.size >= MAX_LIVE_SESSIONS) {
            this.evictLeastRecentlyUsedIdle();
        }

        const row = await prisma.session.findUniqueOrThrow({
            where: { id: sessionId },
            include: { agents: true, skills: true, mcpServers: true, functions: true },
        });
        if (row.userId !== userId) throw new Error("Not authorized to access this session");

        const token = await getGitHubTokenForUser(userId);
        if (!token) throw new Error("No stored GitHub token for this user");

        const [owner, repo] = row.repoFullName.split("/");
        if (!owner || !repo) throw new Error(`Malformed repoFullName on session ${sessionId}: ${row.repoFullName}`);
        const { Octokit } = await import("@octokit/rest");
        const octokit = new Octokit({ auth: token });
        const fsProvider = new GitHubFsProvider(octokit, owner, repo, row.repoDefaultBranch, SESSION_STATE_PATH);

        const pendingPermissions = new Map<string, PendingPermission>();
        const pendingPlans = new Map<string, PendingPlan>();
        const pendingUserInputs = new Map<string, PendingUserInput>();
        // The bridge emits its own `app.*` events into the same persisted/
        // broadcast stream as SDK events (rather than only invoking the SDK
        // handler callback), so the chat UI has one reliable channel to
        // render pending approval/plan/question cards from — independent of
        // whether the SDK also happens to emit a matching session event for
        // a given request kind.
        const bridge: PermissionBridge = {
            requestPermission: (request) =>
                new Promise((resolve) => {
                    const requestId = randomUUID();
                    pendingPermissions.set(requestId, { resolve });
                    this.broadcastAppEvent(sessionId, "app.permission_requested", { ...request, requestId });
                    setTimeout(() => {
                        if (pendingPermissions.delete(requestId)) resolve("timeout");
                    }, PERMISSION_TIMEOUT_MS);
                }),
            requestPlanApproval: (request) =>
                new Promise((resolve) => {
                    const requestId = randomUUID();
                    pendingPlans.set(requestId, { resolve });
                    this.broadcastAppEvent(sessionId, "app.plan_requested", { ...request, requestId });
                    setTimeout(() => {
                        if (pendingPlans.delete(requestId)) resolve("timeout");
                    }, PERMISSION_TIMEOUT_MS);
                }),
            requestUserInput: (request) =>
                new Promise((resolve) => {
                    const requestId = randomUUID();
                    pendingUserInputs.set(requestId, { resolve });
                    this.broadcastAppEvent(sessionId, "app.ask_user_requested", { ...request, requestId });
                    setTimeout(() => {
                        if (pendingUserInputs.delete(requestId)) resolve("timeout");
                    }, PERMISSION_TIMEOUT_MS);
                }),
        };

        const mode = row.mode as SessionMode;
        const customAgents: CustomAgentConfig[] = row.agents.map((a) => ({
            name: a.name,
            displayName: a.displayName ?? undefined,
            description: a.description ?? undefined,
            prompt: a.prompt,
            tools: a.tools.length > 0 ? a.tools : null,
        }));
        const disabledSkills = row.skills.filter((s) => !s.enabled).map((s) => s.skillName);
        const functionTools = row.functions.map((fn) => createFunctionTool(fn));
        const mcpServers: Record<string, MCPServerConfig> = {};
        for (const server of row.mcpServers) {
            const extra = server.encryptedConfig ? (JSON.parse(decryptSecret(server.encryptedConfig)) as Record<string, unknown>) : {};
            mcpServers[server.name] =
                server.type === "http"
                    ? { type: "http", url: server.target, headers: extra.headers as Record<string, string> | undefined }
                    : { type: "stdio", command: server.target, args: extra.args as string[] | undefined, env: extra.env as Record<string, string> | undefined };
        }

        const client = new CopilotClient({
            mode: "empty",
            gitHubToken: token,
            useLoggedInUser: false,
            // Declares path conventions for the virtual filesystem
            // (required by `mode: "empty"`); the actual I/O is handled per
            // *session* by `createSessionFsProvider` below (a
            // `SessionConfigBase` field, not a client option) returning our
            // GitHubFsProvider. Both `initialCwd` and `sessionStatePath` are
            // logical paths inside that virtual filesystem, not real paths
            // on this container's disk — the runtime's own session-state
            // writes land under `sessionStatePath` and are deliberately
            // excluded from `commit_and_push` (see GitHubFsProvider's
            // reservedPrefix) so they never leak into the target repo.
            sessionFs: { initialCwd: "/", sessionStatePath: SESSION_STATE_PATH, conventions: "posix" },
            logLevel: "warning",
        });
        await client.start();

        const live: LiveSession = {
            sessionId,
            mode,
            client,
            fsProvider,
            sockets: new Set(),
            pendingPermissions,
            pendingPlans,
            pendingUserInputs,
            // sdkSession assigned just below; typed as definite-assignment here
            // since createSession/resumeSession both need the handlers above.
            sdkSession: undefined as unknown as CopilotSession,
        };

        const sharedConfig = {
            availableTools: NO_CLONE_AVAILABLE_TOOLS,
            customAgents,
            disabledSkills,
            mcpServers,
            tools: [createCommitAndPushTool(fsProvider, row.repoDefaultBranch), ...functionTools],
            onPermissionRequest: createPermissionHandler(mode, bridge),
            onExitPlanModeRequest: createExitPlanModeHandler(mode, bridge),
            onUserInputRequest: createUserInputHandler(mode, bridge),
            hooks: { onPreToolUse: createPreToolUseGuard() },
            onEvent: (event: SdkSessionEvent) => this.handleSdkEvent(sessionId, event),
            createSessionFsProvider: () => fsProvider,
        };

        live.sdkSession = row.sdkSessionId
            ? await client.resumeSession(row.sdkSessionId, sharedConfig)
            : await client.createSession(sharedConfig);

        if (!row.sdkSessionId) {
            await prisma.session.update({
                where: { id: sessionId },
                data: { sdkSessionId: live.sdkSession.sessionId },
            });
        }

        this.live.set(sessionId, live);
        return live;
    }

    private handleSdkEvent(sessionId: string, event: SdkSessionEvent) {
        this.broadcastAppEvent(sessionId, event.type, event.data);
    }

    /** Persists + fans out one event to every attached socket, whether it
     * originated from the SDK (`handleSdkEvent`) or is one of this app's
     * own synthetic `app.*` events (permission/plan request & resolution).
     * Fire-and-forget: persistence must never block the SDK's event
     * dispatch loop. Failures are logged, not thrown, so one bad write
     * doesn't take the live session down. */
    private broadcastAppEvent(sessionId: string, type: string, data: unknown) {
        appendEvent(sessionId, type, data)
            .then((wireEvent) => {
                const live = this.live.get(sessionId);
                if (!live) return;
                const message: ClientSessionEvent = { kind: "event", event: wireEvent };
                const payload = JSON.stringify(message);
                for (const ws of live.sockets) {
                    if (ws.readyState === ws.OPEN) ws.send(payload);
                }
            })
            .catch((err) => console.error(`Failed to persist event for session ${sessionId}`, err));
    }

    private evict(sessionId: string) {
        const live = this.live.get(sessionId);
        if (!live || live.sockets.size > 0) return;
        this.live.delete(sessionId);
        live.client.stop().catch((err) => console.error(`Failed to stop client for session ${sessionId}`, err));
    }

    private evictLeastRecentlyUsedIdle() {
        for (const [sessionId, live] of this.live) {
            if (live.sockets.size === 0 && live.mode !== "auto") {
                this.evict(sessionId);
                return;
            }
        }
        // Every live slot is either attached or an in-progress auto run;
        // let the new attach queue behind the client's own concurrency
        // rather than force-killing background work.
    }
}

/**
 * The GitHub-API filesystem (github-fs.ts) holds edits in memory until this
 * tool is called, so the agent controls when a batch of edits becomes one
 * commit — rather than committing on every file write — and can choose to
 * push straight to the target branch or open a PR instead.
 */
function createCommitAndPushTool(fsProvider: GitHubFsProvider, defaultBranch: string) {
    return defineTool("commit_and_push", {
        description:
            "Commits every pending file change made so far in this session as a single commit, " +
            "optionally against a new branch and opening a pull request instead of pushing directly.",
        parameters: z.object({
            message: z.string().describe("Commit message"),
            branch: z
                .string()
                .optional()
                .describe(`Branch to push to (defaults to '${defaultBranch}'); created if it doesn't exist`),
            openPullRequest: z
                .object({ title: z.string(), body: z.string().optional() })
                .optional()
                .describe("If set, opens a PR from `branch` into the default branch instead of pushing directly to it"),
        }),
        handler: async ({ message, branch, openPullRequest }) => {
            if (fsProvider.pendingChangeCount === 0) {
                return "Nothing to commit — no pending file changes in this session.";
            }
            const result = await fsProvider.flush({
                message,
                branch,
                openPullRequest: openPullRequest ? { title: openPullRequest.title, body: openPullRequest.body } : undefined,
            });
            return result.pullRequestUrl
                ? `Committed ${result.commitSha} and opened pull request: ${result.pullRequestUrl}`
                : `Committed ${result.commitSha} to ${branch ?? defaultBranch}.`;
        },
    });
}

const FUNCTION_CALL_TIMEOUT_MS = 30_000;

/**
 * A user-defined "function" (src/components/settings, SessionFunction
 * model) is a custom tool backed by an outbound webhook rather than
 * arbitrary code — safe to configure from a mobile settings screen. The
 * tool's JSON Schema `parameters` is the row's `parametersSchema` verbatim
 * (defineTool accepts a raw JSON Schema object as well as a Zod schema);
 * calling it POSTs the arguments to `webhookUrl` and returns the response
 * body as the tool result.
 */
function createFunctionTool(fn: SessionFunction) {
    const extraHeaders = fn.encryptedHeaders
        ? (JSON.parse(decryptSecret(fn.encryptedHeaders)) as { headers: Record<string, string> }).headers
        : undefined;

    return defineTool(fn.name, {
        description: fn.description,
        parameters: fn.parametersSchema as Record<string, unknown>,
        handler: async (args) => {
            const controller = new AbortController();
            const timeout = setTimeout(() => controller.abort(), FUNCTION_CALL_TIMEOUT_MS);
            try {
                const res = await fetch(fn.webhookUrl, {
                    method: "POST",
                    headers: { "Content-Type": "application/json", ...extraHeaders },
                    body: JSON.stringify(args),
                    signal: controller.signal,
                });
                const text = await res.text();
                if (!res.ok) return `Error: webhook returned HTTP ${res.status}: ${text.slice(0, 2000)}`;
                try {
                    return JSON.parse(text);
                } catch {
                    return text;
                }
            } catch (err) {
                const message = err instanceof Error ? err.message : String(err);
                return `Error: failed to call function "${fn.name}": ${message}`;
            } finally {
                clearTimeout(timeout);
            }
        },
    });
}

export const sessionManager = new SessionManager();
