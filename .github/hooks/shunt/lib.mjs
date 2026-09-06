/**
 * Shared helpers for the shunt hooks and delegation scripts.
 *
 * Shunt routes I/O-heavy agent work — bulk file reads and boilerplate
 * generation — to a cheap Azure AI Foundry deployment instead of letting it
 * land in the frontier model's context. Three layers, per the Spotify
 * design this is modelled on:
 *
 *   1. `guard.mjs`   — a `preToolUse` hook that *blocks* oversized reads and
 *                      points the agent at a skill. Local and fast: it never
 *                      touches the network, because hooks run under a short
 *                      `timeoutSec` (10s in hooks.json).
 *   2. `bulk-read.mjs` / `code-write.mjs` — the delegation scripts that do
 *                      talk to Foundry. Invoked *by the agent*, from a skill,
 *                      so their latency is not charged against the hook budget.
 *   3. `.github/skills/*` — tell the agent how to call the scripts.
 *
 * The layering degrades gracefully: even if the agent never reads the skill,
 * the hook still blocks the expensive read.
 */

import { closeSync, existsSync, openSync, readFileSync, readSync, statSync } from "node:fs";
import { resolve, relative, sep } from "node:path";

const CONFIG_PATH = new URL("./shunt.config.json", import.meta.url);

/** Defaults match the reference implementation's published values so the
 * behaviour is recognisable; `shunt.config.json` overrides them per repo and
 * environment variables override that (see README for why this repo raises
 * `minLines`). */
const DEFAULTS = {
    enabled: true,
    /** Sessions the shunt is allowed to fire in, matched against
     * `COPILOT_SESSION_MODE`. Delegation costs a 10-30s round trip, which is
     * dead weight in Interactive but free in Autopilot and scheduled
     * Automations — the sessions where context bloat actually hurts. */
    modes: ["autopilot", "automation"],
    minLines: 350,
    /** Never shunted, and never sent off-box by the delegation scripts. See
     * the content-exclusion warning in README.md. */
    exclude: [],
    /** Ceiling on a single delegated payload, so one enormous file can't be
     * shipped to the worker in full. */
    maxBytes: 400_000,
    /** No 30s ceiling here — unlike a hosted mode runtime, the deployment is
     * yours, so large generations don't have to be split. */
    timeoutMs: 120_000,
};

function readConfigFile() {
    try {
        return JSON.parse(readFileSync(CONFIG_PATH, "utf8"));
    } catch {
        return {};
    }
}

const num = (raw, fallback) => {
    const n = Number(raw);
    return Number.isFinite(n) && n > 0 ? n : fallback;
};

const list = (raw) =>
    raw
        .split(/[:,]/)
        .map((s) => s.trim())
        .filter(Boolean);

export function loadConfig(env = process.env) {
    const file = readConfigFile();
    const cfg = { ...DEFAULTS, ...file };
    if (env.SHUNT_ENABLED !== undefined) cfg.enabled = !/^(0|false|no|off)$/i.test(env.SHUNT_ENABLED);
    if (env.SHUNT_MIN_LINES) cfg.minLines = num(env.SHUNT_MIN_LINES, cfg.minLines);
    if (env.SHUNT_MAX_BYTES) cfg.maxBytes = num(env.SHUNT_MAX_BYTES, cfg.maxBytes);
    if (env.SHUNT_TIMEOUT_MS) cfg.timeoutMs = num(env.SHUNT_TIMEOUT_MS, cfg.timeoutMs);
    if (env.SHUNT_MODES) cfg.modes = list(env.SHUNT_MODES);
    if (env.SHUNT_EXCLUDE) cfg.exclude = [...cfg.exclude, ...list(env.SHUNT_EXCLUDE)];
    return cfg;
}

/**
 * Whether the shunt should fire for this session.
 *
 * When the runtime tells us the session mode we honour `cfg.modes`. When it
 * doesn't — the variable name is not something we can confirm against the
 * hooks reference — we fall back to `enabled` rather than silently doing
 * nothing, and say so on stderr so the fallback is visible in hook logs.
 */
export function isActive(cfg, env = process.env) {
    if (!cfg.enabled) return { active: false, why: "SHUNT_ENABLED is off" };
    const mode = (env.COPILOT_SESSION_MODE ?? env.COPILOT_AGENT_MODE ?? "").toLowerCase();
    if (!mode) return { active: true, why: "session mode unknown; falling back to SHUNT_ENABLED" };
    if (cfg.modes.map((m) => m.toLowerCase()).includes(mode)) return { active: true, why: `mode=${mode}` };
    return { active: false, why: `mode=${mode} not in [${cfg.modes.join(", ")}]` };
}

/**
 * Minimal glob matcher over POSIX-style repo-relative paths. `**` spans
 * separators, `*` and `?` don't, and a leading `**\/` is optional so
 * `**\/*.key` matches `deploy.key` at the root as well as nested copies —
 * the case that decides whether an exclusion actually holds. Enough for the
 * exclusion lists, and avoids pulling a dependency into a hook that has to
 * start fast.
 */
export function matchesGlob(pattern, path) {
    let rx = "";
    for (let i = 0; i < pattern.length; i++) {
        const c = pattern[i];
        if (c === "*" && pattern[i + 1] === "*") {
            if (pattern[i + 2] === "/") {
                rx += "(?:[^/]+/)*"; // zero or more directory segments
                i += 2;
            } else {
                rx += ".*";
                i += 1;
            }
        } else if (c === "*") {
            rx += "[^/]*";
        } else if (c === "?") {
            rx += "[^/]";
        } else {
            rx += /[.+^${}()|[\]\\]/.test(c) ? `\\${c}` : c;
        }
    }
    return new RegExp(`^${rx}$`).test(path);
}

/** Repo-relative, POSIX-separated, for stable glob matching regardless of
 * whether the agent passed an absolute or relative path. */
export function toRepoPath(path, cwd = process.cwd()) {
    const rel = relative(cwd, resolve(cwd, path));
    return rel.split(sep).join("/");
}

export function isExcluded(cfg, path, cwd = process.cwd()) {
    const repoPath = toRepoPath(path, cwd);
    return cfg.exclude.some((p) => matchesGlob(p, repoPath));
}

/**
 * Counts newlines, bailing out as soon as the threshold is exceeded, so a
 * 50MB file costs the same as a 400-line one. Returns `{ lines, overLimit }`
 * where `lines` is a lower bound once `overLimit` is set.
 */
export function countLines(path, limit) {
    let fd;
    try {
        fd = openSync(path, "r");
    } catch {
        return null;
    }
    try {
        const buf = Buffer.allocUnsafe(64 * 1024);
        let lines = 0;
        let bytes = 0;
        let read;
        while ((read = readSync(fd, buf, 0, buf.length, null)) > 0) {
            bytes += read;
            for (let i = 0; i < read; i++) if (buf[i] === 0x0a) lines++;
            if (lines > limit) return { lines, bytes, overLimit: true };
        }
        // A trailing line with no newline still counts as a line.
        if (bytes > 0 && lines === 0) lines = 1;
        return { lines, bytes, overLimit: lines > limit };
    } finally {
        closeSync(fd);
    }
}

export function fileExists(path) {
    try {
        return existsSync(path) && statSync(path).isFile();
    } catch {
        return false;
    }
}
