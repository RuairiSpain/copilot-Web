import type { Octokit } from "@octokit/rest";
import type { SessionFsProvider } from "@github/copilot-sdk";

/**
 * Backs a Copilot SDK session's file tools with the GitHub REST API instead
 * of a local git checkout (the "GitHub API virtual filesystem, no clone"
 * choice — see the plan). Wired in via
 * `CopilotClientOptions.createSessionFsProvider` in session-manager.ts.
 *
 * Trade-off, by design: there is no real working directory, so shell-based
 * tools (npm install, running tests/builds) have nothing to operate on and
 * are left out of each session's `availableTools`. This provider only
 * backs read/write/list/rm/rename-shaped file tools.
 *
 * Reads and directory listings are served from an in-memory snapshot of the
 * repo's git tree (one `getTree` call per session, recursive — cheap even
 * for large repos since it's a single request), overlaid with this
 * session's uncommitted edits so read-after-write is consistent without
 * round-tripping to GitHub. Nothing is pushed to GitHub until `flush()` is
 * called (wired to a `commit_and_push` / `open_pull_request` tool
 * registered alongside this provider), which batches every pending change
 * into a single commit via the Git Data API rather than one commit per
 * file write.
 */

type FsFileInfo = Awaited<ReturnType<SessionFsProvider["stat"]>>;
type FsDirEntry = Awaited<ReturnType<SessionFsProvider["readdirWithTypes"]>>[number];

interface TreeEntry {
    type: "blob" | "tree";
    sha: string;
    size?: number;
}

class EnoentError extends Error {
    code = "ENOENT";
    constructor(path: string) {
        super(`ENOENT: no such file or directory, '${path}'`);
    }
}

export interface GitHubFsFlushResult {
    commitSha: string;
    /** Set when `openPullRequest` was requested and the branch pushed to
     * wasn't the repo's default branch. */
    pullRequestUrl?: string;
}

export interface GitHubFsFlushOptions {
    message: string;
    /** Defaults to the provider's configured branch. Set to push edits to a
     * feature branch instead (created from the base branch if missing). */
    branch?: string;
    openPullRequest?: { title: string; body?: string; base?: string };
}

function normalizePath(input: string): string {
    // The SDK passes POSIX-style paths, sometimes with a leading "/" or
    // "./"; git tree paths never have either. ".." is rejected outright —
    // there's no repo boundary to escape into here, but rejecting keeps the
    // provider's contract honest with the rest of the tool sandboxing.
    const cleaned = input.replace(/^\.?\/+/, "").replace(/\/+$/, "");
    const segments = cleaned.split("/").filter((s) => s.length > 0 && s !== ".");
    if (segments.includes("..")) {
        throw new Error(`Refusing to resolve path outside the repo: '${input}'`);
    }
    return segments.join("/");
}

function parentDirs(path: string): string[] {
    const parts = path.split("/").slice(0, -1);
    const dirs: string[] = [];
    for (let i = 1; i <= parts.length; i++) dirs.push(parts.slice(0, i).join("/"));
    return dirs;
}

export class GitHubFsProvider implements SessionFsProvider {
    private tree: Map<string, TreeEntry> | null = null;
    private loadingTree: Promise<void> | null = null;
    private readonly blobContentCache = new Map<string, string>();
    /** null value = deleted */
    private readonly overlay = new Map<string, string | null>();
    private readonly virtualDirs = new Set<string>();

    constructor(
        private readonly octokit: Octokit,
        private readonly owner: string,
        private readonly repo: string,
        private readonly branch: string,
        /** Path prefix (e.g. the runtime's session-state directory) whose
         * writes are tracked like any other file — so reads/lists see them
         * — but are never included in `flush()`'s commit. */
        private readonly reservedPrefix?: string,
    ) {}

    private isReserved(path: string): boolean {
        return this.reservedPrefix !== undefined && (path === this.reservedPrefix || path.startsWith(`${this.reservedPrefix}/`));
    }

    private async ensureTree(): Promise<Map<string, TreeEntry>> {
        if (this.tree) return this.tree;
        if (!this.loadingTree) {
            this.loadingTree = (async () => {
                const { data: ref } = await this.octokit.git.getRef({
                    owner: this.owner,
                    repo: this.repo,
                    ref: `heads/${this.branch}`,
                });
                const { data: commit } = await this.octokit.git.getCommit({
                    owner: this.owner,
                    repo: this.repo,
                    commit_sha: ref.object.sha,
                });
                const { data: tree } = await this.octokit.git.getTree({
                    owner: this.owner,
                    repo: this.repo,
                    tree_sha: commit.tree.sha,
                    recursive: "true",
                });
                const map = new Map<string, TreeEntry>();
                for (const entry of tree.tree) {
                    if (!entry.path || !entry.sha) continue;
                    if (entry.type === "blob" || entry.type === "tree") {
                        map.set(entry.path, { type: entry.type, sha: entry.sha, size: entry.size });
                    }
                }
                this.tree = map;
            })();
        }
        await this.loadingTree;
        return this.tree!;
    }

    private isDeleted(path: string): boolean {
        return this.overlay.get(path) === null;
    }

    private async dirExists(path: string, tree: Map<string, TreeEntry>): Promise<boolean> {
        if (path === "") return true;
        if (this.virtualDirs.has(path) && !this.isDeleted(path)) return true;
        const entry = tree.get(path);
        if (entry?.type === "tree" && !this.isDeleted(path)) return true;
        // A directory also "exists" implicitly if any live file lives under it.
        for (const key of [...this.overlay.keys(), ...tree.keys()]) {
            if (key.startsWith(`${path}/`) && !this.isDeleted(key)) return true;
        }
        return false;
    }

    async readFile(path: string): Promise<string> {
        const p = normalizePath(path);
        if (this.overlay.has(p)) {
            // Non-null assertion is safe here: `.has()` just confirmed the
            // key exists, so `.get()` can only be `string | null`, not
            // `undefined` — TS can't correlate the two calls on its own.
            const content = this.overlay.get(p)!;
            if (content === null) throw new EnoentError(path);
            return content;
        }
        if (this.blobContentCache.has(p)) return this.blobContentCache.get(p)!;

        const tree = await this.ensureTree();
        const entry = tree.get(p);
        if (!entry || entry.type !== "blob") throw new EnoentError(path);

        const { data: blob } = await this.octokit.git.getBlob({
            owner: this.owner,
            repo: this.repo,
            file_sha: entry.sha,
        });
        const content = Buffer.from(blob.content, blob.encoding as BufferEncoding).toString("utf8");
        this.blobContentCache.set(p, content);
        return content;
    }

    async writeFile(path: string, content: string): Promise<void> {
        const p = normalizePath(path);
        this.overlay.set(p, content);
        this.blobContentCache.delete(p);
        for (const dir of parentDirs(p)) this.virtualDirs.add(dir);
    }

    async appendFile(path: string, content: string): Promise<void> {
        let existing = "";
        try {
            existing = await this.readFile(path);
        } catch {
            // File doesn't exist yet — append behaves like write.
        }
        await this.writeFile(path, existing + content);
    }

    async exists(path: string): Promise<boolean> {
        const p = normalizePath(path);
        if (this.overlay.has(p)) return !this.isDeleted(p);
        const tree = await this.ensureTree();
        if (tree.has(p)) return true;
        return this.dirExists(p, tree);
    }

    async stat(path: string): Promise<FsFileInfo> {
        const p = normalizePath(path);
        const tree = await this.ensureTree();

        // This provider has no real mtime/birthtime source (no local
        // filesystem, and the GitHub API's commit timestamps aren't fetched
        // per-file here) — "now" for both is a defensible placeholder
        // since nothing in this app reads them for anything but display.
        const now = new Date().toISOString();

        if (this.overlay.has(p) && !this.isDeleted(p)) {
            const content = this.overlay.get(p) as string;
            return { isDirectory: false, isFile: true, size: Buffer.byteLength(content, "utf8"), mtime: now, birthtime: now };
        }

        if (await this.dirExists(p, tree)) {
            return { isDirectory: true, isFile: false, size: 0, mtime: now, birthtime: now };
        }

        const entry = tree.get(p);
        if (entry?.type === "blob" && !this.isDeleted(p)) {
            return { isDirectory: false, isFile: true, size: entry.size ?? 0, mtime: now, birthtime: now };
        }

        throw new EnoentError(path);
    }

    async mkdir(path: string): Promise<void> {
        // Git has no concept of an empty directory; this only affects this
        // session's in-memory view until a file is actually written under
        // it, at which point the directory becomes "real" on flush.
        this.virtualDirs.add(normalizePath(path));
    }

    async readdir(path: string): Promise<string[]> {
        const entries = await this.readdirWithTypes(path);
        return entries.map((e) => e.name);
    }

    async readdirWithTypes(path: string): Promise<FsDirEntry[]> {
        const p = normalizePath(path);
        const tree = await this.ensureTree();
        if (p !== "" && !(await this.dirExists(p, tree))) throw new EnoentError(path);

        const prefix = p === "" ? "" : `${p}/`;
        const children = new Map<string, boolean>(); // name -> isDirectory

        const consider = (key: string, isDir: boolean) => {
            if (!key.startsWith(prefix) || key === p) return;
            const rest = key.slice(prefix.length);
            const name = rest.split("/")[0];
            if (!name) return;
            const directChild = !rest.includes("/");
            if (this.isDeleted(key) && directChild) return;
            children.set(name, directChild ? isDir : true);
        };

        for (const [key, entry] of tree) consider(key, entry.type === "tree");
        for (const key of this.overlay.keys()) consider(key, false);
        for (const dir of this.virtualDirs) consider(dir, true);

        return [...children.entries()]
            .filter(([name]) => !this.isDeleted(`${prefix}${name}`))
            .map(([name, isDirectory]) => ({ name, type: isDirectory ? "directory" : "file" }) as FsDirEntry);
    }

    async rm(path: string, recursive: boolean, force: boolean): Promise<void> {
        const p = normalizePath(path);
        const tree = await this.ensureTree();
        const isDir = await this.dirExists(p, tree);

        if (isDir) {
            if (!recursive) throw new Error(`Cannot remove non-empty directory '${path}' without recursive`);
            for (const key of [...tree.keys(), ...this.overlay.keys()]) {
                if (key === p || key.startsWith(`${p}/`)) this.overlay.set(key, null);
            }
            this.virtualDirs.delete(p);
            return;
        }

        if (!(await this.exists(p))) {
            if (force) return;
            throw new EnoentError(path);
        }
        this.overlay.set(p, null);
        this.blobContentCache.delete(p);
    }

    async rename(src: string, dest: string): Promise<void> {
        const from = normalizePath(src);
        const to = normalizePath(dest);
        const tree = await this.ensureTree();

        if (await this.dirExists(from, tree)) {
            const prefix = `${from}/`;
            const keys = new Set([...tree.keys(), ...this.overlay.keys()].filter((k) => k.startsWith(prefix)));
            for (const key of keys) {
                const content = await this.readFile(key).catch(() => null);
                if (content === null) continue;
                const newKey = to + key.slice(from.length);
                this.overlay.set(newKey, content);
                for (const dir of parentDirs(newKey)) this.virtualDirs.add(dir);
                this.overlay.set(key, null);
            }
            this.virtualDirs.delete(from);
            this.virtualDirs.add(to);
            return;
        }

        const content = await this.readFile(from);
        await this.writeFile(to, content);
        this.overlay.set(from, null);
        this.blobContentCache.delete(from);
    }

    /** Number of pending (uncommitted), committable file changes in this
     * session — excludes the runtime's own reserved session-state path. */
    get pendingChangeCount(): number {
        return [...this.overlay.keys()].filter((p) => !this.isReserved(p)).length;
    }

    /**
     * Batches every pending write/delete into a single commit via the Git
     * Data API (blobs → tree → commit → ref update), optionally against a
     * feature branch and opening a PR — rather than one commit per file
     * tool call. Called from the `commit_and_push` tool registered in
     * session-manager.ts, not by the SDK directly.
     */
    async flush(options: GitHubFsFlushOptions): Promise<GitHubFsFlushResult> {
        const committable = [...this.overlay.entries()].filter(([path]) => !this.isReserved(path));
        if (committable.length === 0) {
            throw new Error("Nothing to commit — no pending file changes in this session.");
        }
        const targetBranch = options.branch ?? this.branch;

        const baseRef = await this.octokit.git.getRef({
            owner: this.owner,
            repo: this.repo,
            ref: `heads/${this.branch}`,
        });

        if (targetBranch !== this.branch) {
            const branchExists = await this.octokit.git
                .getRef({ owner: this.owner, repo: this.repo, ref: `heads/${targetBranch}` })
                .then(() => true)
                .catch(() => false);
            if (!branchExists) {
                await this.octokit.git.createRef({
                    owner: this.owner,
                    repo: this.repo,
                    ref: `refs/heads/${targetBranch}`,
                    sha: baseRef.data.object.sha,
                });
            }
        }

        const baseCommitSha =
            targetBranch === this.branch
                ? baseRef.data.object.sha
                : (
                      await this.octokit.git.getRef({
                          owner: this.owner,
                          repo: this.repo,
                          ref: `heads/${targetBranch}`,
                      })
                  ).data.object.sha;
        const baseCommit = await this.octokit.git.getCommit({
            owner: this.owner,
            repo: this.repo,
            commit_sha: baseCommitSha,
        });

        const treeEntries = await Promise.all(
            committable.map(async ([path, content]) => {
                if (content === null) {
                    return { path, mode: "100644" as const, type: "blob" as const, sha: null };
                }
                const { data: blob } = await this.octokit.git.createBlob({
                    owner: this.owner,
                    repo: this.repo,
                    content: Buffer.from(content, "utf8").toString("base64"),
                    encoding: "base64",
                });
                return { path, mode: "100644" as const, type: "blob" as const, sha: blob.sha };
            }),
        );

        const { data: newTree } = await this.octokit.git.createTree({
            owner: this.owner,
            repo: this.repo,
            base_tree: baseCommit.data.tree.sha,
            tree: treeEntries,
        });

        const { data: newCommit } = await this.octokit.git.createCommit({
            owner: this.owner,
            repo: this.repo,
            message: options.message,
            tree: newTree.sha,
            parents: [baseCommitSha],
        });

        await this.octokit.git.updateRef({
            owner: this.owner,
            repo: this.repo,
            ref: `heads/${targetBranch}`,
            sha: newCommit.sha,
        });

        for (const [path] of committable) this.overlay.delete(path);
        this.tree = null; // force a fresh snapshot on next read, picking up the new commit

        let pullRequestUrl: string | undefined;
        if (options.openPullRequest) {
            const { data: pr } = await this.octokit.pulls.create({
                owner: this.owner,
                repo: this.repo,
                title: options.openPullRequest.title,
                body: options.openPullRequest.body,
                head: targetBranch,
                base: options.openPullRequest.base ?? this.branch,
            });
            pullRequestUrl = pr.html_url;
        }

        return { commitSha: newCommit.sha, pullRequestUrl };
    }
}
