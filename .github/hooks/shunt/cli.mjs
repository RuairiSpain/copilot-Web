/**
 * Argument handling and payload assembly shared by the delegation scripts.
 */

import { readFileSync } from "node:fs";
import { isExcluded, fileExists, toRepoPath } from "./lib.mjs";

/** `--flag value` and `--flag a b c` (list-valued flags collect until the
 * next `--flag`). Deliberately tiny — these scripts are called by an agent
 * with a fixed shape documented in the skills, not by a human exploring. */
export function parseArgs(argv) {
    const out = {};
    let current = null;
    for (const token of argv) {
        if (token.startsWith("--")) {
            current = token.slice(2);
            out[current] = out[current] ?? [];
        } else if (current) {
            out[current].push(token);
        }
    }
    return out;
}

export const one = (args, name) => (args[name]?.length ? args[name].join(" ") : null);
export const many = (args, name) => args[name] ?? [];

export function die(message) {
    process.stderr.write(`shunt: ${message}\n`);
    process.exit(1);
}

/**
 * Reads files for delegation, enforcing two guards.
 *
 * The exclusion check is the important one: these scripts ship file contents
 * to an endpoint *outside* Copilot's pipeline, so anything the platform would
 * have withheld under content exclusion has to be withheld here too. It fails
 * loudly rather than skipping the file, because a silently-dropped file
 * produces a confidently wrong answer. See README.md — this list is a local
 * mirror of policy, not the policy itself.
 */
export function collectFiles(cfg, paths) {
    if (paths.length === 0) die("no --paths given");

    const excluded = paths.filter((p) => isExcluded(cfg, p));
    if (excluded.length > 0) {
        die(
            `refusing to delegate excluded path(s): ${excluded.map((p) => toRepoPath(p)).join(", ")}\n` +
                `  These are withheld from the worker model by shunt.config.json "exclude".\n` +
                `  Read them directly instead.`,
        );
    }

    const missing = paths.filter((p) => !fileExists(p));
    if (missing.length > 0) die(`no such file(s): ${missing.join(", ")}`);

    let total = 0;
    return paths.map((path) => {
        const content = readFileSync(path, "utf8");
        total += Buffer.byteLength(content, "utf8");
        if (total > cfg.maxBytes) {
            die(`payload exceeds ${cfg.maxBytes} bytes at ${toRepoPath(path)} — split the call across fewer files`);
        }
        return { path: toRepoPath(path), content };
    });
}

/** XML tags give the worker unambiguous file boundaries, which matters more
 * than it looks when several files are concatenated into one prompt. */
export function asXml(files) {
    return files.map((f) => `<file path="${f.path}">\n${f.content}\n</file>`).join("\n\n");
}
