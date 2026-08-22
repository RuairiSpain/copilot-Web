# Copilot Web

A mobile-friendly, multi-user website for chatting with [GitHub Copilot](https://github.com/github/copilot-sdk)
across your repos. Sign in with GitHub, pick (or create) a repo, and start a
session with its own mode — **planning**, **interactive**, or **auto accept**
— plus its own custom agents, skills, MCP servers, and webhook-backed
functions. Sessions persist server-side, so they're there when you log back
in from another device, and **auto**-mode sessions keep working even while
you're offline.

## How it works

- **One always-on Node.js process** (`server.ts`) hosts both the Next.js app
  and a WebSocket server (`/ws/sessions/:id`), because `@github/copilot-sdk`
  spawns a native runtime per session and keeps state in-process — it can't
  run on serverless/edge.
- **No git clone.** Each session's file tools are backed by the GitHub REST
  API instead of a local checkout (`src/server/github-fs.ts`), via the SDK's
  `createSessionFsProvider` session hook. Edits stay in memory until the
  agent calls the `commit_and_push` tool, which batches them into one commit
  (optionally opening a PR). The trade-off: there's no working directory, so
  shell-based tools (installing deps, running tests/builds) aren't available
  in this pass — see **Known limitations** below.
- **Modes map onto SDK primitives** (`src/server/permission-modes.ts`):
  planning gates on `onExitPlanModeRequest`, interactive forwards every
  `onPermissionRequest` over the session's WebSocket and blocks for a human
  (and enables the agent's `ask_user` tool via `onUserInputRequest`), auto
  uses the SDK's `approveAll` plus a hard-coded deny for destructive shell
  patterns. `onUserInputRequest` is deliberately left unregistered in auto
  mode so `ask_user` is simply unavailable there, rather than a question
  nobody can ever answer.
- **Persistence** is a Postgres-backed event log (`SessionEvent`), not the
  SDK's in-memory history — a client (re)connecting, possibly from a
  different device, replays it before subscribing live. This is also what
  lets an auto-mode session keep going with nobody attached.
- **Chat UI**: Next.js (App Router) + Tailwind, with
  [`@assistant-ui/react`](https://github.com/assistant-ui/assistant-ui)'s
  `useExternalStoreRuntime`/`AssistantRuntimeProvider` and
  `ThreadPrimitive`/`ComposerPrimitive` driving a thread built from the
  session's event stream (`src/components/chat/*`), including interactive
  approve/deny, plan-review, and ask-user cards.
- **Functions**: per-session custom tools backed by an outbound webhook
  (`SessionFunction` — name, JSON Schema parameters, URL, optional auth
  header) rather than arbitrary code, since a mobile settings screen can't
  safely author a real tool handler. The agent calls it, this app POSTs the
  arguments to the URL, and the response becomes the tool result.

## Getting started

```bash
cp .env.example .env
# fill in AUTH_GITHUB_ID / AUTH_GITHUB_SECRET (GitHub OAuth App, callback
# http://localhost:3000/api/auth/callback/github, scope `repo`),
# AUTH_SECRET (npx auth secret), and TOKEN_ENCRYPTION_KEY (openssl rand -base64 32)

docker compose up   # app + Postgres
# or, locally:
npm install
npm run prisma:migrate
npm run dev

# to run the E2E suite too (needs a Chromium build once):
npx playwright install chromium
npm run test:e2e
```

## Scripts

| Command | Purpose |
| --- | --- |
| `npm run dev` | Custom server (Next.js + WS) in dev mode |
| `npm run build` / `npm run start` | Production build/run |
| `npm run lint` / `npm run typecheck` | ESLint / `tsc --noEmit` |
| `npm test` | Vitest tests (unit + Postgres integration — see Status) |
| `npm run test:e2e` | Playwright E2E suite (`e2e/` — see Status) |
| `npm run prisma:migrate` / `prisma:deploy` | DB migrations (dev / prod) |

## Known limitations (by design, this pass)

- **No shell/build tools.** The GitHub-API filesystem has no real working
  directory, so `availableTools` (`src/server/session-manager.ts`)
  deliberately excludes `bash`/shell-dependent tools. Good for doc/code
  edits and PRs; not for "run the test suite." The exact built-in tool
  catalog available under `CopilotClientMode: "empty"` is a best-effort list
  (`NO_CLONE_AVAILABLE_TOOLS`) — verify it against the installed CLI's real
  tool names (e.g. by inspecting `tool.execution_start` events from a live
  session) and adjust.
- **Single replica.** `CopilotClient`/`CopilotSession` state lives in the
  one Node process; there's no cross-replica session affinity yet. Fine for
  a single Docker container; revisit before scaling out horizontally.
- Concurrent live sessions are capped (`MAX_LIVE_SESSIONS`, default 8); idle
  interactive/planning sessions are evicted after 30 minutes with nobody
  attached. Auto-mode sessions are exempt — that's the point.
- **Not yet run against a real Copilot subscription.** Everything below has
  been verified against a real Postgres and a real (locally-signed) session
  cookie; the one thing that's structurally impossible to verify without
  external credentials is an actual GitHub OAuth App + a GitHub account with
  Copilot access. See "What's verified" below for exactly where that
  boundary is.

## Status: what's verified vs. what still needs real credentials

**Verified with real infrastructure, not just typechecking:**

- `npm run typecheck`, `npm run lint`, `npm run build` all pass, checked
  against the *installed* `@github/copilot-sdk` and `@assistant-ui/react`
  type definitions (not just their docs).
- `npm test` — 36 tests, including integration tests against a real local
  Postgres (`tests/event-log.test.ts`, `tests/session-manager.test.ts` —
  the latter mocks only `@github/copilot-sdk`'s native runtime and
  `@octokit/rest`, since neither can run without a real Copilot
  subscription/GitHub token; every call *into* the mock is still
  type-checked against the real SDK).
- `npm run test:e2e` — a Playwright suite (`e2e/`) that drives the real app
  in a real browser end-to-end: sign in → create a session against a repo →
  send a prompt → see the assistant's reply render → the session shows up
  back on the list. Two things are mocked, both explained in `e2e/`: login
  (a session cookie is minted directly with Auth.js's own `encode()` rather
  than driving GitHub's real OAuth screen — see `e2e/global-setup.ts`) and
  the session WebSocket's "backend" (`page.routeWebSocket`, so the suite
  doesn't need a live `@github/copilot-sdk` runtime or real Copilot access
  — see `e2e/mocks.ts`). Everything else — routing, session create/list
  APIs, the real DB row, the chat UI's event-to-message rendering, the
  composer — is real. This suite is also what caught a genuine bug:
  `server.ts`'s WebSocket upgrade handler was destroying *every* upgrade
  request that wasn't one of ours, including `next dev`'s own Turbopack/HMR
  socket, which silently stalled hydration entirely in dev mode. Fixed by
  forwarding non-matching upgrades to `app.getUpgradeHandler()` instead
  (`src/server/ws.ts`) — a dev-only symptom, but a real bug, found by
  actually running a browser against the app rather than by reasoning about
  the code.
- A full local smoke test booting the real server against a real Postgres
  database (migrated with `prisma migrate deploy`), covering: unauthenticated
  requests correctly redirected/401'd on every page and API route; the
  WebSocket route correctly rejecting an unauthenticated upgrade (`401`); a
  real signed Auth.js session cookie authenticating successfully; the full
  session CRUD lifecycle end-to-end against the DB (create → list → get
  detail → chat page render → settings page render → delete requires
  confirm → delete succeeds → 404 after); and a WebSocket attach with a
  real auth cookie failing *gracefully* (a clear close code, no server
  crash, server stays responsive) at exactly the point where it needs a
  real GitHub token to decrypt.

**Still needs real credentials to verify** (a real GitHub OAuth App, a
GitHub account with Copilot access, and — for auto mode — time to observe
a session actually keep running unattended): the GitHub OAuth login
redirect itself; the repo picker/creator against the real GitHub API;
Copilot actually receiving a prompt, replying, and calling tools; a file
edit round-tripping through the GitHub API and landing in a real commit/PR;
and an auto-mode session genuinely continuing to work with no client
attached. None of these are things any amount of local testing without
those credentials can close — they're the natural boundary of what's
verifiable in this environment.
