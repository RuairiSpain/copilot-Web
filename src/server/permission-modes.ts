import { approveAll, type ExitPlanModeHandler, type PermissionHandler } from "@github/copilot-sdk";
import type { PermissionDecisionKind, SessionMode } from "@/types/session";

/**
 * Maps the app's three user-facing modes onto the SDK's permission/plan
 * primitives (see docs/auth + nodejs/docs/extensions.md, and
 * `PermissionHandler` / `ExitPlanModeHandler` in src/types.ts):
 *
 * - planning:    read-only until the user approves a plan
 *                (`onExitPlanModeRequest`).
 * - interactive: every tool permission is forwarded to whichever client(s)
 *                are attached and awaited (`onPermissionRequest`).
 * - auto:        approved automatically so the session keeps working while
 *                nobody's connected — the "work in the background even if
 *                I'm offline" mode — with a hard-coded deny for
 *                obviously destructive shell patterns as a safety net.
 */

/** What SessionManager exposes to bridge a permission/plan request to
 * whichever WebSocket client(s) are currently attached to a session, and
 * to correlate their eventual `permission.respond` / `plan.respond`
 * message back to the pending request. */
export interface PermissionBridge {
    requestPermission(request: {
        requestId: string;
        kind: string;
        [key: string]: unknown;
    }): Promise<PermissionDecisionKind | "timeout">;

    requestPlanApproval(request: {
        requestId: string;
        [key: string]: unknown;
    }): Promise<{ approved: boolean; feedback?: string } | "timeout">;
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
        const decision = await bridge.requestPermission(request as unknown as { requestId: string; kind: string });
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
        const result = await bridge.requestPlanApproval(request as unknown as { requestId: string });
        if (result === "timeout" || !result.approved) {
            return { approved: false, feedback: result === "timeout" ? "Timed out waiting for review." : result.feedback };
        }
        return { approved: true };
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
