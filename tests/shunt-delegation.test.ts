import { execFile } from "node:child_process";
import { createServer, type Server } from "node:http";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { promisify } from "node:util";
import { afterAll, beforeAll, describe, expect, it } from "vitest";

/**
 * Tests for the delegation scripts against a stub Foundry endpoint. The worker
 * response is canned; what's under test is everything around it — the payload
 * the scripts build, the exclusion re-check that keeps withheld files off the
 * wire, and the on-disk write path that keeps generated code out of the
 * agent's context.
 */

const BULK_READ = resolve(".github/hooks/shunt/bulk-read.mjs");
const CODE_WRITE = resolve(".github/hooks/shunt/code-write.mjs");

let workdir: string;
let server: Server;
let endpoint: string;
/** Every request body the stub worker received, so tests can assert on what
 * was actually sent rather than only on what came back. */
let received: Array<Record<string, never>>;
let reply = "";

/** Spawned asynchronously on purpose: the stub server below shares this
 * thread's event loop, so a synchronous spawn would deadlock — the child's
 * request could never be served while the parent blocked on the child. */
const execFileAsync = promisify(execFile);

async function run(script: string, args: string[], env: Record<string, string> = {}) {
    const options = {
        cwd: workdir,
        encoding: "utf8" as const,
        env: {
            ...process.env,
            FOUNDRY_ENDPOINT: endpoint,
            FOUNDRY_DEPLOYMENT: "worker-mini",
            FOUNDRY_API_KEY: "stub-key",
            SHUNT_EXCLUDE: "secrets/**,**/*.key",
            ...env,
        },
    };
    try {
        const { stdout, stderr } = await execFileAsync(process.execPath, [script, ...args], options);
        return { stdout, stderr, status: 0 };
    } catch (err) {
        const e = err as { stdout?: string; stderr?: string; code?: number };
        return { stdout: e.stdout ?? "", stderr: e.stderr ?? "", status: e.code ?? -1 };
    }
}

const lastPrompt = () => {
    const body = received.at(-1) as unknown as { messages: Array<{ role: string; content: string }> };
    return body.messages.find((m) => m.role === "user")!.content;
};

beforeAll(async () => {
    workdir = mkdtempSync(join(tmpdir(), "shunt-deleg-"));
    mkdirSync(join(workdir, "secrets"), { recursive: true });
    writeFileSync(join(workdir, "service.ts"), "export const a = 1;\n");
    writeFileSync(join(workdir, "handler.ts"), "export const b = 2;\n");
    writeFileSync(join(workdir, "reference.test.ts"), "import { it } from 'vitest';\n");
    writeFileSync(join(workdir, "secrets/token.ts"), "export const secret = 'x';\n");

    received = [];
    server = createServer((req, res) => {
        const chunks: Buffer[] = [];
        req.on("data", (c) => chunks.push(c));
        req.on("end", () => {
            received.push(JSON.parse(Buffer.concat(chunks).toString()));
            res.writeHead(200, { "Content-Type": "application/json" });
            res.end(
                JSON.stringify({
                    choices: [{ message: { content: reply } }],
                    usage: { prompt_tokens: 1200, completion_tokens: 80 },
                }),
            );
        });
    });
    await new Promise<void>((done) => server.listen(0, "127.0.0.1", done));
    const addr = server.address() as { port: number };
    endpoint = `http://127.0.0.1:${addr.port}`;
});

afterAll(() => {
    server.closeAllConnections();
    server.close();
    rmSync(workdir, { recursive: true, force: true });
});

describe("bulk-read", () => {
    it("sends every file wrapped in its own tag and prints the answer", async () => {
        reply = "- `doStuff()` — writes to Postgres";
        const { stdout, stderr, status } = await run(BULK_READ, [
            "--question",
            "Which methods touch the database?",
            "--paths",
            "service.ts",
            "handler.ts",
        ]);

        expect(status).toBe(0);
        expect(stdout.trim()).toBe(reply);
        const prompt = lastPrompt();
        expect(prompt).toContain('<file path="service.ts">');
        expect(prompt).toContain('<file path="handler.ts">');
        expect(prompt).toContain("<question>");
        // The saving is only credible if it's observable.
        expect(stderr).toContain("1200 in / 80 out");
    });

    it("refuses to put an excluded file on the wire", async () => {
        const before = received.length;
        const { stderr, status } = await run(BULK_READ, ["--question", "what is this", "--paths", "secrets/token.ts"]);

        expect(status).toBe(1);
        expect(stderr).toContain("refusing to delegate excluded path");
        expect(received.length).toBe(before);
    });

    it("stops before sending a payload over the byte ceiling", async () => {
        const before = received.length;
        const { stderr, status } = await run(BULK_READ, ["--question", "q", "--paths", "service.ts"], { SHUNT_MAX_BYTES: "5" });

        expect(status).toBe(1);
        expect(stderr).toContain("payload exceeds");
        expect(received.length).toBe(before);
    });

    it("fails on a missing file rather than silently answering from fewer", async () => {
        expect((await run(BULK_READ, ["--question", "q", "--paths", "nope.ts"])).stderr).toContain("no such file");
    });
});

describe("code-write", () => {
    it("writes generated code straight to disk, stripping fences", async () => {
        reply = "```ts\nimport { it } from 'vitest';\nit('works', () => {});\n```";
        const { stdout, stderr, status } = await run(CODE_WRITE, [
            "--spec",
            "a passing test",
            "--reference",
            "reference.test.ts",
            "--target",
            "generated/new.test.ts",
        ]);

        expect(status).toBe(0);
        const written = readFileSync(join(workdir, "generated/new.test.ts"), "utf8");
        expect(written).toBe("import { it } from 'vitest';\nit('works', () => {});\n");
        expect(stderr).toContain("never in agent context");
        // The agent gets a pointer, not the code — that's the output-token saving.
        expect(stdout).not.toContain("vitest");
        expect(stdout).toContain("Review it before committing");
    });

    it("requires a reference so the worker has a pattern to match", async () => {
        const { stderr, status } = await run(CODE_WRITE, ["--spec", "some tests"]);
        expect(status).toBe(1);
        expect(stderr).toContain("--reference");
    });

    it("refuses to generate into an excluded path", async () => {
        const { stderr, status } = await run(CODE_WRITE, [
            "--spec",
            "a key",
            "--reference",
            "reference.test.ts",
            "--target",
            "deploy.key",
        ]);
        expect(status).toBe(1);
        expect(stderr).toContain("refusing to generate into excluded path");
    });

    it("prints to stdout when no target is given", async () => {
        reply = "const x = 1;";
        const { stdout } = await run(CODE_WRITE, ["--spec", "a const", "--reference", "reference.test.ts"]);
        expect(stdout.trim()).toBe("const x = 1;");
    });
});

describe("foundry client", () => {
    it("reports a useful error when the worker fails", async () => {
        const { stderr, status } = await run(BULK_READ, ["--question", "q", "--paths", "service.ts"], {
            FOUNDRY_ENDPOINT: "http://127.0.0.1:1",
        });
        expect(status).toBe(1);
        expect(stderr).toContain("worker request failed");
    });

    it("refuses to run without credentials", async () => {
        const { stderr, status } = await run(BULK_READ, ["--question", "q", "--paths", "service.ts"], { FOUNDRY_API_KEY: "" });
        expect(status).toBe(1);
        expect(stderr).toContain("FOUNDRY_API_KEY");
    });
});
