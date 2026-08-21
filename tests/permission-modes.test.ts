import { describe, expect, it, vi } from "vitest";
import {
    createExitPlanModeHandler,
    createPermissionHandler,
    createPreToolUseGuard,
    type PermissionBridge,
} from "@/server/permission-modes";

function makeBridge(overrides: Partial<PermissionBridge> = {}): PermissionBridge {
    return {
        requestPermission: vi.fn(async () => "reject" as const),
        requestPlanApproval: vi.fn(async () => ({ approved: false })),
        ...overrides,
    };
}

describe("createPermissionHandler", () => {
    it("auto mode approves ordinary requests without asking anyone", async () => {
        const bridge = makeBridge();
        const handler = createPermissionHandler("auto", bridge);
        const result = await handler({ requestId: "1", kind: "write" } as never, {} as never);
        expect(result).toEqual({ kind: "approve-once" });
        expect(bridge.requestPermission).not.toHaveBeenCalled();
    });

    it("auto mode still blocks an obviously destructive shell command", async () => {
        const bridge = makeBridge();
        const handler = createPermissionHandler("auto", bridge);
        const result = await handler(
            { requestId: "1", kind: "shell", fullCommandText: "rm -rf /" } as never,
            {} as never,
        );
        expect(result).toEqual({ kind: "reject" });
    });

    it("interactive mode forwards the request and awaits the bridge's decision", async () => {
        const bridge = makeBridge({ requestPermission: vi.fn(async () => "approve-once" as const) });
        const handler = createPermissionHandler("interactive", bridge);
        const result = await handler({ requestId: "42", kind: "write" } as never, {} as never);
        expect(bridge.requestPermission).toHaveBeenCalledWith({ requestId: "42", kind: "write" });
        expect(result).toEqual({ kind: "approve-once" });
    });

    it("interactive mode denies by default when the bridge times out (offline client)", async () => {
        const bridge = makeBridge({ requestPermission: vi.fn(async () => "timeout" as const) });
        const handler = createPermissionHandler("interactive", bridge);
        const result = await handler({ requestId: "42", kind: "write" } as never, {} as never);
        expect(result).toEqual({ kind: "reject" });
    });

    it("planning mode also forwards to the bridge like interactive", async () => {
        const bridge = makeBridge({ requestPermission: vi.fn(async () => "reject" as const) });
        const handler = createPermissionHandler("planning", bridge);
        const result = await handler({ requestId: "1", kind: "read" } as never, {} as never);
        expect(result).toEqual({ kind: "reject" });
    });
});

describe("createExitPlanModeHandler", () => {
    it("auto-approves immediately for interactive/auto sessions (no plan gating)", async () => {
        const bridge = makeBridge();
        for (const mode of ["interactive", "auto"] as const) {
            const handler = createExitPlanModeHandler(mode, bridge);
            const result = await handler({ requestId: "1" } as never, {} as never);
            expect(result).toEqual({ approved: true });
        }
        expect(bridge.requestPlanApproval).not.toHaveBeenCalled();
    });

    it("planning mode blocks on the bridge and relays approval", async () => {
        const bridge = makeBridge({ requestPlanApproval: vi.fn(async () => ({ approved: true })) });
        const handler = createExitPlanModeHandler("planning", bridge);
        await expect(handler({ requestId: "9" } as never, {} as never)).resolves.toEqual({ approved: true });
        expect(bridge.requestPlanApproval).toHaveBeenCalled();
    });

    it("planning mode denies with feedback when the bridge times out", async () => {
        const bridge = makeBridge({ requestPlanApproval: vi.fn(async () => "timeout" as const) });
        const handler = createExitPlanModeHandler("planning", bridge);
        const result = await handler({ requestId: "9" } as never, {} as never);
        expect(result.approved).toBe(false);
    });
});

describe("createPreToolUseGuard", () => {
    it("denies destructive bash commands regardless of mode", () => {
        const guard = createPreToolUseGuard();
        const result = guard({ toolName: "bash", toolArgs: { command: "rm -rf /" } });
        expect(result).toMatchObject({ permissionDecision: "deny" });
    });

    it("leaves ordinary tool calls alone", () => {
        const guard = createPreToolUseGuard();
        expect(guard({ toolName: "bash", toolArgs: { command: "ls -la" } })).toBeUndefined();
        expect(guard({ toolName: "edit", toolArgs: { path: "a.ts" } })).toBeUndefined();
    });
});
