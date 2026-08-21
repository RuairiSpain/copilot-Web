# Copilot Web

A mobile-friendly, multi-user website for chatting with [GitHub Copilot](https://github.com/github/copilot-sdk)
across your repos. Sign in with GitHub, pick (or create) a repo, and start a
session with its own mode — **planning**, **interactive**, or **auto accept**
— plus its own custom agents, skills, and MCP servers. Sessions persist
server-side, so they're there when you log back in from another device, and
**auto**-mode sessions keep working even while you're offline.

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
  `onPermissionRequest` over the session's WebSocket and blocks for a human,
  auto uses the SDK's `approveAll` plus a hard-coded deny for destructive
  shell patterns.
- **Persistence** is a Postgres-backed event log (`SessionEvent`), not the
  SDK's in-memory history — a client (re)connecting, possibly from a
  different device, replays it before subscribing live. This is also what
  lets an auto-mode session keep going with nobody attached.
- **Chat UI**: Next.js (App Router) + Tailwind, with
  [`@assistant-ui/react`](https://github.com/assistant-ui/assistant-ui)'s
  `useExternalStoreRuntime`/`AssistantRuntimeProvider` and
  `ThreadPrimitive`/`ComposerPrimitive` driving a thread built from the
  session's event stream (`src/components/chat/*`), including interactive
  approve/deny and plan review cards.

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
```

## Scripts

| Command | Purpose |
| --- | --- |
| `npm run dev` | Custom server (Next.js + WS) in dev mode |
| `npm run build` / `npm run start` | Production build/run |
| `npm run lint` / `npm run typecheck` | ESLint / `tsc --noEmit` |
| `npm test` | Vitest unit tests |
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
- PWA icons (`public/icons/icon-192.png` / `icon-512.png`, referenced by the
  manifest route) aren't included — add real ones for a proper home-screen
  icon; the app installs and runs fine without them.

## Status

`npm run typecheck`, `npm run lint`, `npm test` (18 tests), and
`npm run build` all pass as of this commit, checked against the real
`@github/copilot-sdk` and `@assistant-ui/react` type definitions (not just
their docs) after `npm install`.
