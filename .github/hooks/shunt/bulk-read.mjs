#!/usr/bin/env node
/**
 * Delegates a multi-file read to the Foundry worker deployment and prints a
 * structured answer. The files never enter the agent's context.
 *
 *   node .github/hooks/shunt/bulk-read.mjs \
 *     --question "What does this service do?" \
 *     --paths src/server/session-manager.ts src/server/github-fs.ts
 *
 * Scope, deliberately: this buys *understanding*, never edits. Worker
 * summaries carry no reliable line numbers, so when the agent needs to change
 * something it must re-read that section directly with an explicit
 * offset/limit — which the hook lets through.
 */

import { loadConfig } from "./lib.mjs";
import { invoke, FoundryError } from "./foundry.mjs";
import { parseArgs, one, many, die, collectFiles, asXml } from "./cli.mjs";

const SYSTEM = [
    "You are a precise code analyst.",
    "Read the provided files and answer the question concisely.",
    "Output structured bullets only. No greetings, no prose, no preambles.",
    "Lead every bullet with the exact name, type, or line number.",
    "Use nested bullets for details. Skip anything the caller did not ask for.",
    "If the files do not contain the answer, say so in one bullet rather than guessing.",
].join(" ");

async function main() {
    const args = parseArgs(process.argv.slice(2));
    const question = one(args, "question");
    if (!question) die('missing --question "..."');

    const cfg = loadConfig();
    const files = collectFiles(cfg, many(args, "paths"));

    const answer = await invoke({
        system: SYSTEM,
        user: `${asXml(files)}\n\n<question>\n${question}\n</question>`,
        cfg,
    });
    process.stdout.write(answer.trim() + "\n");
}

main().catch((err) => die(err instanceof FoundryError ? err.message : `bulk-read failed: ${err?.message ?? err}`));
