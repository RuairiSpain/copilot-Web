#!/usr/bin/env node
/**
 * `preToolUse` hook: blocks bulk file reads so they get delegated to a cheap
 * worker model instead of entering the frontier model's context.
 *
 * Registered once in `.github/hooks/hooks.json` and dispatching internally on
 * tool name, rather than as two hook entries — one process per tool call
 * instead of two, which matters because this runs before *every* tool call.
 * The two checks are still named separately (`check-file-size`,
 * `check-bash-read`) in the decision it emits, so hook logs stay readable.
 *
 * What passes through, deliberately:
 *   - targeted reads (`offset`/`limit`/`view_range`) — the agent already
 *     knows the section it wants, and delegation can't serve edits anyway
 *     because worker summaries carry no reliable line numbers;
 *   - piped or redirected shell reads (`cat f | grep x`) — also targeted;
 *   - `head`/`tail` with an explicit count — bounded by construction;
 *   - anything under `exclude` (see the content-exclusion note in README).
 *
 * Contract note: the two documented ways for a `preToolUse` hook to block are
 * a non-zero exit and a `permissionDecision` in JSON on stdout. This emits
 * BOTH so it denies under either, and `SHUNT_DENY_EXIT` tunes the exit code
 * if the live runtime disagrees. That is the one thing to verify against the
 * hooks reference before trusting this in anger.
 */

import { loadConfig, isActive, isExcluded, countLines, fileExists, toRepoPath } from "./lib.mjs";

const READ_TOOLS = new Set(["read", "view", "read_file", "readfile", "str_replace_editor"]);
const SHELL_TOOLS = new Set(["bash", "shell", "run", "execute", "run_command"]);
const PAGERS = ["cat", "head", "tail", "less", "more"];

/** Field names the runtime might use for a path; `toolInput` is documented
 * loosely enough (a bare string for bash, an object elsewhere) that this
 * stays defensive rather than assuming one shape. */
const PATH_KEYS = ["path", "file_path", "filePath", "filename", "file", "target", "absolute_path"];
const RANGE_KEYS = ["offset", "limit", "view_range", "start_line", "end_line", "startLine", "endLine"];

function allow(check, reason) {
    // Silence on the happy path keeps hook logs usable; the reason is only
    // interesting when SHUNT_DEBUG is set.
    if (process.env.SHUNT_DEBUG) process.stderr.write(`shunt/${check}: allow (${reason})\n`);
    process.stdout.write(JSON.stringify({ permissionDecision: "allow" }) + "\n");
    process.exit(0);
}

function deny(check, reason) {
    process.stdout.write(JSON.stringify({ permissionDecision: "deny", permissionDecisionReason: reason }) + "\n");
    process.stderr.write(`shunt/${check}: deny — ${reason}\n`);
    const code = Number(process.env.SHUNT_DENY_EXIT ?? 2);
    process.exit(Number.isInteger(code) ? code : 2);
}

function parseInput(raw) {
    let payload;
    try {
        payload = JSON.parse(raw);
    } catch {
        return null;
    }
    const toolName = String(payload.toolName ?? payload.tool_name ?? "").toLowerCase();
    const input = payload.toolInput ?? payload.tool_input ?? payload.toolArgs ?? {};
    return { toolName, input };
}

function pickPath(input) {
    if (typeof input === "string") return input.trim() || null;
    if (input && typeof input === "object") {
        for (const key of PATH_KEYS) if (typeof input[key] === "string" && input[key].trim()) return input[key].trim();
    }
    return null;
}

function isTargetedRead(input) {
    if (!input || typeof input !== "object") return false;
    return RANGE_KEYS.some((k) => input[k] !== undefined && input[k] !== null);
}

function pickCommand(input) {
    if (typeof input === "string") return input;
    if (input && typeof input === "object") {
        for (const key of ["command", "cmd", "script", "commandLine"]) {
            if (typeof input[key] === "string") return input[key];
        }
    }
    return "";
}

/** Reports the first file in `paths` that is over the line threshold, or
 * null if every one of them is small, missing, or excluded. */
function findOversized(cfg, paths) {
    for (const path of paths) {
        if (isExcluded(cfg, path)) continue;
        if (!fileExists(path)) continue;
        const counted = countLines(path, cfg.minLines);
        if (counted?.overLimit) return { path, ...counted };
    }
    return null;
}

function checkFileSize(cfg, input) {
    const check = "check-file-size";
    if (isTargetedRead(input)) allow(check, "targeted read (offset/limit)");
    const path = pickPath(input);
    if (!path) allow(check, "no path in tool input");
    if (isExcluded(cfg, path)) allow(check, `excluded: ${toRepoPath(path)}`);
    if (!fileExists(path)) allow(check, "not a readable file");
    const counted = countLines(path, cfg.minLines);
    if (!counted?.overLimit) allow(check, `${counted?.lines ?? 0} lines <= ${cfg.minLines}`);
    deny(
        check,
        `${toRepoPath(path)} is over ${cfg.minLines} lines. Do not read it in full — use the bulk-reader skill:\n` +
            `  node .github/hooks/shunt/bulk-read.mjs --question "<your question>" --paths ${toRepoPath(path)}\n` +
            `If you need a specific section to edit, re-read it with an explicit offset/limit instead.`,
    );
}

/**
 * Splits a shell command on separators and reports the pager invocations
 * worth inspecting. A pipe or redirect anywhere in a segment makes it a
 * targeted read, so the segment is skipped.
 */
function pagerReads(command) {
    return command
        .split(/(?:&&|\|\||;|\n)/)
        .filter((seg) => !/[|><]/.test(seg))
        .map((seg) => seg.trim().split(/\s+/).filter(Boolean))
        .filter((argv) => argv.length > 1 && PAGERS.includes(argv[0]))
        .filter((argv) => !(/^(head|tail)$/.test(argv[0]) && argv.some((a) => /^-(n|c)$|^-\d+$/.test(a))))
        .map((argv) => argv.slice(1).filter((a) => !a.startsWith("-")));
}

function checkBashRead(cfg, input) {
    const check = "check-bash-read";
    const command = pickCommand(input);
    if (!command) allow(check, "no command in tool input");
    const candidates = pagerReads(command).flat();
    if (candidates.length === 0) allow(check, "no unbounded pager read");
    const hit = findOversized(cfg, candidates);
    if (!hit) allow(check, "pager targets are small, missing, or excluded");
    deny(
        check,
        `${toRepoPath(hit.path)} is over ${cfg.minLines} lines. Do not cat it — use the bulk-reader skill:\n` +
            `  node .github/hooks/shunt/bulk-read.mjs --question "<your question>" --paths ${toRepoPath(hit.path)}`,
    );
}

export function decide(cfg, toolName, input) {
    if (READ_TOOLS.has(toolName)) return checkFileSize(cfg, input);
    if (SHELL_TOOLS.has(toolName)) return checkBashRead(cfg, input);
    return allow("dispatch", `tool ${toolName || "(unnamed)"} is not read-like`);
}

async function main() {
    const chunks = [];
    for await (const chunk of process.stdin) chunks.push(chunk);
    const parsed = parseInput(Buffer.concat(chunks).toString("utf8"));
    if (!parsed) allow("dispatch", "unparseable hook input");

    const cfg = loadConfig();
    const { active, why } = isActive(cfg);
    if (!active) allow("dispatch", `inactive: ${why}`);

    decide(cfg, parsed.toolName, parsed.input);
}

main().catch((err) => {
    // Fail open: a broken hook must never wedge the agent. The cost of a
    // missed shunt is tokens; the cost of a hard block is a stuck session.
    process.stderr.write(`shunt: hook error, allowing — ${err?.message ?? err}\n`);
    process.stdout.write(JSON.stringify({ permissionDecision: "allow" }) + "\n");
    process.exit(0);
});
