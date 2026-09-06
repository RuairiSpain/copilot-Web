#!/usr/bin/env node
/**
 * Delegates boilerplate generation to the Foundry worker deployment.
 *
 *   node .github/hooks/shunt/code-write.mjs \
 *     --spec "Vitest suite for github-fs flush()" \
 *     --reference tests/event-log.test.ts \
 *     --target tests/github-fs-flush.test.ts
 *
 * With --target the code goes straight to disk and the agent never sees it,
 * which is where most of the saving is: without delegation the agent pays to
 * read the references *and* emits the file as output tokens, billed several
 * times input rates.
 *
 * --reference is required. Without a file to match patterns against the
 * worker produces context-free code that fits nothing in the project.
 */

import { writeFileSync, mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { loadConfig, isExcluded, toRepoPath } from "./lib.mjs";
import { invoke, stripFences, FoundryError } from "./foundry.mjs";
import { parseArgs, one, many, die, collectFiles, asXml } from "./cli.mjs";

const SYSTEM = [
    "You generate code files based on a spec and reference files.",
    "Match the existing patterns, conventions, naming, and style exactly.",
    "Output only the code — no explanations, no markdown fences.",
    "If the spec is ambiguous, make reasonable choices that match the reference code's patterns.",
].join(" ");

async function main() {
    const args = parseArgs(process.argv.slice(2));
    const spec = one(args, "spec");
    if (!spec) die('missing --spec "..."');

    const references = many(args, "reference");
    if (references.length === 0) die("missing --reference <path> (required: the worker needs a pattern to match)");

    const target = one(args, "target");
    const cfg = loadConfig();

    // Generating *into* an excluded path would route safety-critical code
    // through the worker just as surely as reading one would.
    if (target && isExcluded(cfg, target)) die(`refusing to generate into excluded path: ${toRepoPath(target)}`);

    const files = collectFiles(cfg, references);
    const code = stripFences(
        await invoke({
            system: SYSTEM,
            user: `${asXml(files)}\n\n<spec>\n${spec}\n</spec>\n\n<target>${target ?? "(stdout)"}</target>`,
            cfg,
        }),
    );

    if (!target) {
        process.stdout.write(code);
        return;
    }
    mkdirSync(dirname(target), { recursive: true });
    writeFileSync(target, code, "utf8");
    process.stderr.write(`shunt: wrote ${toRepoPath(target)} (${code.split("\n").length} lines, never in agent context)\n`);
    process.stdout.write(`Wrote ${toRepoPath(target)}. Review it before committing.\n`);
}

main().catch((err) => die(err instanceof FoundryError ? err.message : `code-write failed: ${err?.message ?? err}`));
