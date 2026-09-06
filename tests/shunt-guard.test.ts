import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";

/**
 * Behavioural tests for the shunt `preToolUse` hook
 * (`.github/hooks/shunt/guard.mjs`). The hook is spawned as a real child
 * process with JSON on stdin, because that is exactly how the Copilot runtime
 * invokes it — asserting on the process contract (stdout decision + exit code)
 * is the only thing that proves the hook actually blocks.
 *
 * `minLines` and `exclude` come from environment overrides rather than the
 * repo's `shunt.config.json`, so these stay stable if the repo retunes its
 * threshold.
 */

const GUARD = resolve(".github/hooks/shunt/guard.mjs");
const MIN_LINES = 50;

let workdir: string;

function makeFile(name: string, lines: number) {
    const path = join(workdir, name);
    mkdirSync(resolve(path, ".."), { recursive: true });
    writeFileSync(path, Array.from({ length: lines }, (_, i) => `line ${i}`).join("\n"));
    return path;
}

interface Decision {
    permissionDecision: "allow" | "deny";
    permissionDecisionReason?: string;
}

function runHook(payload: unknown, env: Record<string, string> = {}) {
    let stdout: string;
    let status = 0;
    try {
        stdout = execFileSync(process.execPath, [GUARD], {
            input: typeof payload === "string" ? payload : JSON.stringify(payload),
            cwd: workdir,
            encoding: "utf8",
            stdio: ["pipe", "pipe", "pipe"],
            env: {
                ...process.env,
                SHUNT_MIN_LINES: String(MIN_LINES),
                SHUNT_EXCLUDE: "secrets/**,**/*.key",
                ...env,
            },
        });
    } catch (err) {
        // A deny exits non-zero by design, so the throw *is* the result.
        const e = err as { stdout?: string; status?: number };
        stdout = e.stdout ?? "";
        status = e.status ?? -1;
    }
    return { decision: JSON.parse(stdout.trim()) as Decision, status };
}

beforeAll(() => {
    workdir = mkdtempSync(join(tmpdir(), "shunt-"));
    makeFile("big.ts", 400);
    makeFile("small.ts", 10);
    makeFile("secrets/big.ts", 400);
    makeFile("deploy.key", 400);
});

afterAll(() => rmSync(workdir, { recursive: true, force: true }));

describe("check-file-size", () => {
    it("denies a full read of a file over the threshold", () => {
        const { decision, status } = runHook({ toolName: "read", toolInput: { path: "big.ts" } });
        expect(decision.permissionDecision).toBe("deny");
        expect(decision.permissionDecisionReason).toContain("bulk-read.mjs");
        // Both block mechanisms fire: the JSON decision and a non-zero exit.
        expect(status).not.toBe(0);
    });

    it("allows a file under the threshold", () => {
        expect(runHook({ toolName: "read", toolInput: { path: "small.ts" } }).decision.permissionDecision).toBe("allow");
    });

    it("allows a targeted read of a large file, since edits need real line numbers", () => {
        const payload = { toolName: "read", toolInput: { path: "big.ts", offset: 100, limit: 40 } };
        expect(runHook(payload).decision.permissionDecision).toBe("allow");
    });

    it("allows excluded paths through rather than delegating them", () => {
        expect(runHook({ toolName: "read", toolInput: { path: "secrets/big.ts" } }).decision.permissionDecision).toBe("allow");
        expect(runHook({ toolName: "read", toolInput: { path: "deploy.key" } }).decision.permissionDecision).toBe("allow");
    });

    it("allows a path that does not exist", () => {
        expect(runHook({ toolName: "read", toolInput: { path: "nope.ts" } }).decision.permissionDecision).toBe("allow");
    });

    it("reads the path out of alternative field names", () => {
        expect(runHook({ toolName: "view", toolInput: { file_path: "big.ts" } }).decision.permissionDecision).toBe("deny");
    });
});

describe("check-bash-read", () => {
    const bash = (command: string) => runHook({ toolName: "bash", toolInput: { command } });

    it("denies cat on a large file", () => {
        expect(bash("cat big.ts").decision.permissionDecision).toBe("deny");
    });

    it("allows a piped read, which is already targeted", () => {
        expect(bash("cat big.ts | grep session").decision.permissionDecision).toBe("allow");
    });

    it("allows head/tail with an explicit count", () => {
        expect(bash("head -n 20 big.ts").decision.permissionDecision).toBe("allow");
        expect(bash("tail -50 big.ts").decision.permissionDecision).toBe("allow");
    });

    it("denies unbounded head", () => {
        expect(bash("head big.ts").decision.permissionDecision).toBe("deny");
    });

    it("inspects every segment of a compound command", () => {
        expect(bash("npm run lint && cat big.ts").decision.permissionDecision).toBe("deny");
    });

    it("accepts a bare string toolInput", () => {
        expect(runHook({ toolName: "bash", toolInput: "cat big.ts" }).decision.permissionDecision).toBe("deny");
    });

    it("ignores commands that are not pager reads", () => {
        expect(bash("npm test").decision.permissionDecision).toBe("allow");
    });
});

describe("activation", () => {
    it("does nothing when disabled", () => {
        const { decision } = runHook({ toolName: "read", toolInput: { path: "big.ts" } }, { SHUNT_ENABLED: "0" });
        expect(decision.permissionDecision).toBe("allow");
    });

    it("stays out of interactive sessions, where the round trip is dead weight", () => {
        const payload = { toolName: "read", toolInput: { path: "big.ts" } };
        expect(runHook(payload, { COPILOT_SESSION_MODE: "interactive" }).decision.permissionDecision).toBe("allow");
        expect(runHook(payload, { COPILOT_SESSION_MODE: "autopilot" }).decision.permissionDecision).toBe("deny");
    });

    it("ignores tools that are not read-like", () => {
        expect(runHook({ toolName: "write", toolInput: { path: "big.ts" } }).decision.permissionDecision).toBe("allow");
    });

    it("fails open on unparseable input rather than wedging the session", () => {
        const { decision, status } = runHook("not json at all");
        expect(decision.permissionDecision).toBe("allow");
        expect(status).toBe(0);
    });
});
