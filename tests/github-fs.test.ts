import { describe, expect, it, vi } from "vitest";
import type { Octokit } from "@octokit/rest";
import { GitHubFsProvider } from "@/server/github-fs";

function b64(s: string) {
    return Buffer.from(s, "utf8").toString("base64");
}

function makeOctokit() {
    const blobs = new Map<string, string>([["blob-readme", b64("hello world")]]);
    const tree = [
        { path: "README.md", type: "blob", sha: "blob-readme", size: 11 },
        { path: "src", type: "tree", sha: "tree-src" },
        { path: "src/index.ts", type: "blob", sha: "blob-index", size: 20 },
    ];
    blobs.set("blob-index", b64("console.log(1)"));

    const calls: Record<string, unknown[]> = {};
    const record = (name: string, args: unknown) => (calls[name] ??= []).push(args);
    const branches = new Set(["heads/main"]);

    const octokit = {
        git: {
            getRef: vi.fn(async (args: { ref: string }) => {
                record("getRef", args);
                if (!branches.has(args.ref)) throw new Error("Not Found");
                return { data: { object: { sha: "commit-1" } } };
            }),
            getCommit: vi.fn(async (args) => {
                record("getCommit", args);
                return { data: { tree: { sha: "tree-1" } } };
            }),
            getTree: vi.fn(async (args) => {
                record("getTree", args);
                return { data: { tree } };
            }),
            getBlob: vi.fn(async (args: { file_sha: string }) => {
                record("getBlob", args);
                const content = blobs.get(args.file_sha);
                if (!content) throw new Error("blob not found");
                return { data: { content, encoding: "base64" } };
            }),
            createBlob: vi.fn(async (args: { content: string }) => {
                record("createBlob", args);
                return { data: { sha: `new-blob-${Object.keys(calls.createBlob ?? []).length}` } };
            }),
            createTree: vi.fn(async (args) => {
                record("createTree", args);
                return { data: { sha: "new-tree" } };
            }),
            createCommit: vi.fn(async (args) => {
                record("createCommit", args);
                return { data: { sha: "new-commit" } };
            }),
            updateRef: vi.fn(async (args) => {
                record("updateRef", args);
                return { data: {} };
            }),
            createRef: vi.fn(async (args: { ref: string }) => {
                record("createRef", args);
                branches.add(args.ref.replace(/^refs\//, ""));
                return { data: {} };
            }),
        },
        pulls: {
            create: vi.fn(async (args) => {
                record("createPr", args);
                return { data: { html_url: `https://github.com/o/r/pull/1` } };
            }),
        },
    };

    return { octokit: octokit as unknown as Octokit, calls };
}

describe("GitHubFsProvider", () => {
    it("reads existing files from the repo tree", async () => {
        const { octokit } = makeOctokit();
        const fs = new GitHubFsProvider(octokit, "o", "r", "main");
        await expect(fs.readFile("README.md")).resolves.toBe("hello world");
        await expect(fs.readFile("src/index.ts")).resolves.toBe("console.log(1)");
    });

    it("throws ENOENT for a missing file", async () => {
        const { octokit } = makeOctokit();
        const fs = new GitHubFsProvider(octokit, "o", "r", "main");
        await expect(fs.readFile("nope.txt")).rejects.toMatchObject({ code: "ENOENT" });
    });

    it("read-after-write returns the overlay content without a network call", async () => {
        const { octokit, calls } = makeOctokit();
        const fs = new GitHubFsProvider(octokit, "o", "r", "main");
        await fs.writeFile("notes.md", "draft");
        await expect(fs.readFile("notes.md")).resolves.toBe("draft");
        expect(calls.getBlob).toBeUndefined();
    });

    it("exists/stat reflect pending writes and deletes before flush", async () => {
        const { octokit } = makeOctokit();
        const fs = new GitHubFsProvider(octokit, "o", "r", "main");

        await expect(fs.exists("README.md")).resolves.toBe(true);
        await fs.rm("README.md", false, false);
        await expect(fs.exists("README.md")).resolves.toBe(false);

        await fs.writeFile("new/file.txt", "content");
        await expect(fs.exists("new/file.txt")).resolves.toBe(true);
        const stat = await fs.stat("new/file.txt");
        expect(stat.isFile).toBe(true);
        expect(stat.isDirectory).toBe(false);
    });

    it("readdir lists both committed and pending entries at a path", async () => {
        const { octokit } = makeOctokit();
        const fs = new GitHubFsProvider(octokit, "o", "r", "main");
        await fs.writeFile("src/new.ts", "x");

        const entries = await fs.readdirWithTypes("src");
        const names = entries.map((e) => e.name).sort();
        expect(names).toEqual(["index.ts", "new.ts"]);
    });

    it("flush batches pending changes into a single commit and clears the overlay", async () => {
        const { octokit, calls } = makeOctokit();
        const fs = new GitHubFsProvider(octokit, "o", "r", "main");
        await fs.writeFile("a.txt", "one");
        await fs.writeFile("b.txt", "two");

        const result = await fs.flush({ message: "test commit" });

        expect(result.commitSha).toBe("new-commit");
        expect(calls.createBlob).toHaveLength(2);
        expect(calls.createTree).toHaveLength(1);
        expect(calls.createCommit).toHaveLength(1);
        expect(calls.updateRef).toHaveLength(1);
        expect(fs.pendingChangeCount).toBe(0);
    });

    it("flush with openPullRequest creates a branch and opens a PR", async () => {
        const { octokit, calls } = makeOctokit();
        const fs = new GitHubFsProvider(octokit, "o", "r", "main");
        await fs.writeFile("a.txt", "one");

        const result = await fs.flush({
            message: "feat: add a.txt",
            branch: "copilot/add-a",
            openPullRequest: { title: "Add a.txt" },
        });

        expect(calls.createRef).toHaveLength(1);
        expect(calls.createPr).toHaveLength(1);
        expect(result.pullRequestUrl).toBe("https://github.com/o/r/pull/1");
    });

    it("flush throws when there is nothing pending", async () => {
        const { octokit } = makeOctokit();
        const fs = new GitHubFsProvider(octokit, "o", "r", "main");
        await expect(fs.flush({ message: "noop" })).rejects.toThrow(/Nothing to commit/);
    });
});
