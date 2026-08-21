import { approveAll, type ExitPlanModeHandler, type PermissionHandler, type SessionConfigBase } from "@github/copilot-sdk";
import type { PermissionDecisionKind, SessionMode } from "@/types/session";

// `UserInputHandler` isn't re-exported from the package root (unlike
// `PermissionHandler`/`ExitPlanModeHandler`) — derive it structurally from
// the exported `SessionConfigBase` field instead of importing a name that
// doesn't exist at the public API boundary.
type UserInputHandler = NonNullable<SessionConfigBase["onUserInputRequest"]>;

/**
 * Maps the app's three user-facing modes onto the SDK's permission/plan/
 * ask-user primitives (`PermissionHandler` / `ExitPlanModeHandler` /
 * `UserInputHandler` in `@github/copilot-sdk`'s types.ts):
 *
 * - planning:    read-only until the user approves a plan
 *                (`onExitPlanModeRequest`).
 * - interactive: every tool permission is forwarded to whichever client(s)
 *                are attached and awaited (`onPermissionRequest`), and the
 *                agent can ask free-form questions (`onUserInputRequest`).
 * - auto:        approved automatically so the session keeps working while
 *                nobody's connected — the "work in the background even if
 *                I'm offline" mode — with a hard-coded deny for
 *                obviously destructive shell patterns as a safety net.
 *                `onUserInputRequest` is intentionally left unregistered in
 *                auto mode: leaving it unset disables the agent's
 *                `ask_user` tool entirely, rather than leaving a question
 *                that can never be answered by anyone.
 */

/** What SessionManager exposes to bridge a permission/plan/user-input
 * request to whichever WebSocket client(s) are currently attached to a
 * session, and to correlate their eventual `*.respond` message back to the
 * pending request. None of the SDK's request types carry a `requestId` of
 * their own (that only exists on the wrapping session *event*, e.g.
 * `PermissionRequestedData`) — bridge implementations mint their own id
 * per pending request. */
export interface PermissionBridge {
    requestPermission(request: { kind: string; [key: string]: unknown }): Promise<PermissionDecisionKind | "timeout">;

    requestPlanApproval(request: {
        summary: string;
        planContent?: string;
        actions: string[];
        recommendedAction: string;
    }): Promise<{ approved: boolean; selectedAction?: string; feedback?: string } | "timeout">;

    requestUserInput(request: {
        question: string;
        choices?: string[];
        allowFreeform?: boolean;
    }): Promise<{ answer: string; wasFreeform: boolean } | "timeout">;
}

const DESTRUCTIVE_SHELL_PATTERN = /\brm\s+-rf\s+\/|\bmkfs\b|:\(\)\{.*:\|:.*\};:/i;

/** Builds one of the SDK's real `PermissionDecision` variants (see
 * `PermissionDecisionApproveOnce`/`PermissionDecisionApproveForSession`/
 * `PermissionDecisionReject` in `@github/copilot-sdk`'s generated RPC
 * types) — returned as a switch rather than a generic `{ kind }` object so
 * each branch's literal `kind` lines up with its variant exactly. */
function toPermissionResult(decision: PermissionDecisionKind | "timeout") {
    switch (decision) {
        case "approve-once":
            return { kind: "approve-once" as const };
        case "approve-for-session":
            return { kind: "approve-for-session" as const };
        case "reject":
        case "timeout":
            // No client connected (or nobody answered in time) — fail safe
            // by denying rather than silently acting on the repo.
            return { kind: "reject" as const };
    }
}

export function createPermissionHandler(mode: SessionMode, bridge: PermissionBridge): PermissionHandler {
    if (mode === "auto") {
        return (request, invocation) => {
            const command =
                request.kind === "shell" && "fullCommandText" in request
                    ? String((request as { fullCommandText?: unknown }).fullCommandText ?? "")
                    : "";
            if (command && DESTRUCTIVE_SHELL_PATTERN.test(command)) {
                return { kind: "reject" };
            }
            return approveAll(request, invocation);
        };
    }

    // planning + interactive: forward every request and wait for a human.
    return async (request) => {
        const decision = await bridge.requestPermission(request as unknown as { kind: string });
        return toPermissionResult(decision);
    };
}

export function createExitPlanModeHandler(mode: SessionMode, bridge: PermissionBridge): ExitPlanModeHandler {
    if (mode !== "planning") {
        // Interactive/auto sessions don't opt into the CLI's plan-first
        // workflow, so there's nothing to gate — let it proceed.
        return () => ({ approved: true });
    }
    return async (request) => {
        const result = await bridge.requestPlanApproval(request);
        if (result === "timeout" || !result.approved) {
            return { approved: false, feedback: result === "timeout" ? "Timed out waiting for review." : result.feedback };
        }
        return { approved: true, selectedAction: result.selectedAction };
    };
}

/** Only registered for planning/interactive — see the module doc for why
 * auto mode deliberately leaves `ask_user` disabled. */
export function createUserInputHandler(mode: SessionMode, bridge: PermissionBridge): UserInputHandler | undefined {
    if (mode === "auto") return undefined;
    return async (request) => {
        const result = await bridge.requestUserInput(request);
        if (result === "timeout") return { answer: "", wasFreeform: true };
        return result;
    };
}

/** `onPreToolUse` hook, registered for every mode as defense-in-depth
 * (auto mode's permission handler already blocks these, but hooks run
 * regardless of how a tool call was authorized). */
export function createPreToolUseGuard() {
    return (input: { toolName: string; toolArgs: unknown }) => {
        if (input.toolName === "bash" || input.toolName === "shell") {
            const args = input.toolArgs as Record<string, unknown> | undefined;
            const command = String(args?.command ?? "");
            if (DESTRUCTIVE_SHELL_PATTERN.test(command)) {
                return {
                    permissionDecision: "deny" as const,
                    permissionDecisionReason: "Destructive shell command blocked.",
                };
            }
        }
        return undefined;
    };
}
