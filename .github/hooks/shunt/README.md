# shunt — delegate bulk I/O to a cheap Foundry worker

Most of what a coding agent does isn't thinking, it's I/O: reading five files
to answer a question about one method, generating a test file that looks like
the twenty next to it. Those tokens go to a frontier model that is wildly
overqualified for them.

`shunt` intercepts that work and sends it to a cheap Azure AI Foundry
deployment instead. The files never enter the agent's context.

It is a port of the [Shunt plugin][shunt] Spotify built for Claude Code on top
of Portal/AiKA modes, retargeted at Foundry and GitHub Copilot's own hook and
skill surfaces. The interesting parts are the boundaries, not the plumbing —
read *[What it deliberately does not do](#what-it-deliberately-does-not-do)*
before switching it on.

[shunt]: https://github.com/spotify/portal-ai-plugins

## How it works

Three layers, so it degrades gracefully — even if the agent never reads the
skill, the hook still blocks the expensive read.

| Layer | File | Runs |
| --- | --- | --- |
| 1. Hook | `guard.mjs` | Before every tool call, via `.github/hooks/hooks.json`. Local only, no network — hooks run under a 10s `timeoutSec`. |
| 2. Scripts | `bulk-read.mjs`, `code-write.mjs` | Invoked *by the agent*, so their 10–30s round trip isn't charged against the hook budget. |
| 3. Skills | `.github/skills/{bulk-reader,code-writer}/SKILL.md` | Loaded when relevant; tells the agent the exact invocation. |

The hook denies a full read of anything over `minLines` and names the script to
use instead. Targeted reads (`offset`/`limit`), piped shell reads
(`cat f | grep x`), and `head`/`tail` with an explicit count all pass through —
they're already cheap, and delegation can't serve edits anyway.

## Setup

**1. Deploy a worker model in Foundry.** Any cheap chat deployment. This is the
model that reads your code, so pick it accordingly.

**2. Point the scripts at it.**

```bash
export FOUNDRY_ENDPOINT=https://<resource>.services.ai.azure.com
export FOUNDRY_DEPLOYMENT=<worker-deployment-name>
export FOUNDRY_API_KEY=<key>          # or FOUNDRY_TOKEN for managed identity
```

`FOUNDRY_TOKEN` wins over `FOUNDRY_API_KEY`, so a managed-identity setup
(`az account get-access-token --resource https://cognitiveservices.azure.com`)
needs no key at all. Setting `FOUNDRY_API_VERSION` switches from the Azure
OpenAI v1 route to the older dated `deployments/{name}` one.

**3. Optionally put the frontier model in Foundry too.** The Copilot app's BYOK
provider takes a Foundry endpoint directly (Settings → BYOK), so both ends can
bill through one resource — and behind APIM you get per-session token quotas
that Copilot's own credit system doesn't expose.

**4. Verify the hook loads.** `/skills reload` in a CLI session, then
`/skills info bulk-reader`.

## Configuration

`shunt.config.json` sets repo defaults; environment variables override it.

| Key | Env | Default | Notes |
| --- | --- | --- | --- |
| `enabled` | `SHUNT_ENABLED` | `true` | `0`/`false`/`off` disables entirely. |
| `modes` | `SHUNT_MODES` | `autopilot,automation` | Matched against `COPILOT_SESSION_MODE`. |
| `minLines` | `SHUNT_MIN_LINES` | `350` (repo: `800`) | Below this, delegation costs more than it saves. |
| `exclude` | `SHUNT_EXCLUDE` | see config | Never shunted, never sent off-box. |
| `maxBytes` | `SHUNT_MAX_BYTES` | `400000` | Ceiling on one delegated payload. |
| `timeoutMs` | `SHUNT_TIMEOUT_MS` | `120000` | Worker request timeout. |
| — | `SHUNT_DEBUG` | unset | Log allow decisions too. |
| — | `SHUNT_DENY_EXIT` | `2` | See *Verify the deny contract*. |

**Why this repo raises `minLines` to 800.** 350 is the reference default, tuned
against a Java monorepo. Measured here, the largest source files are
`session-manager.ts` (444 lines) and `github-fs.ts` (428) — so at 800 the only
file in the repo that trips the hook is `package-lock.json`, and at 350 it would
start delegating reads the frontier model should simply do.

That is worth stating plainly rather than tuning around: **on a codebase this
size the shunt has almost nothing to do.** It earns its keep on large repos with
genuinely big files, which is where the original was measured. Set the threshold
from measurements on the repo you're pointing it at, not from this default.

**Why it only fires in Autopilot and Automations.** Each delegation is a 10–30
second round trip. That's dead weight in Interactive, where a human is waiting,
and close to free in unattended sessions — which are also where context bloat
does the most damage, because a compaction mid-run costs you the thread of the
task. If the runtime doesn't set `COPILOT_SESSION_MODE`, the hook falls back to
`SHUNT_ENABLED` and says so on stderr.

## Two things to check before trusting this

**Verify the deny contract.** A `preToolUse` hook can block via a non-zero exit
or via a `permissionDecision` in JSON on stdout. `guard.mjs` emits both, so it
denies under either reading, and `SHUNT_DENY_EXIT` retunes the exit code. Watch
one real denial in a live session and confirm the read was actually blocked
rather than merely logged.

**The exclusion list is a mirror of policy, not the policy.** This is the part
that matters in an org. Copilot honours content exclusion configured at the
enterprise, organization, and repository level — but these scripts read files
off disk and POST them to *your* Foundry endpoint, entirely outside that
pipeline. Anything the platform would withhold has to be listed in `exclude`
here as well, or the shunt is a hole in a policy someone believes is enforced.
The same applies in the other direction: `code-write` puts generated code on
disk without it passing Copilot's duplicate-detection filter, so review what it
writes. `exclude` fails loudly rather than skipping a file, because a silently
dropped file produces a confidently wrong answer.

## What it deliberately does not do

- **It can't delegate editing.** Worker summaries carry no reliable line
  numbers. When the agent needs to change code it re-reads that section with an
  explicit `offset`/`limit`, which the hook allows. Delegation buys
  understanding, not edits.
- **It can't delegate reasoning.** A cheap model finds surface patterns and
  misses subtle bugs — the kind of race or lifetime error that's the whole
  reason you're paying for the good model. Debugging, architecture, and
  safety-critical code stay with the frontier model; keep those paths in
  `exclude`.
- **It isn't free below the threshold.** The round trip costs more than a small
  read saves. That's what `minLines` is for.

## Measuring it

The headline number for the original was ~90% on *bulk reads*, not on sessions.
Your saving is that number times however much of your agent work is I/O, so
measure before and after:

- `/chronicle cost tips` in the Copilot app for the session-level baseline.
- `shunt:` lines on stderr for per-delegation worker usage.
- Task success rate on your existing suites. **If success drops, the token
  number doesn't matter.**

One caveat when modelling the saving: without shunt, a large file sits in a
stable prompt prefix and bills at cache-read rates on later turns, so the
real-money delta is smaller than the raw token delta implies. The context
saving — fewer compactions in long unattended runs — is the more reliable win.

## Tests

`tests/shunt-guard.test.ts` spawns the hook as a real process with JSON on
stdin and asserts on the decision and exit code. `tests/shunt-delegation.test.ts`
runs the scripts against a stub Foundry endpoint and asserts on what actually
went over the wire — including that excluded files never do.

```bash
npx vitest run tests/shunt-guard.test.ts tests/shunt-delegation.test.ts
```
