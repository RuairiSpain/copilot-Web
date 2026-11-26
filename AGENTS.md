# Project instructions

Copilot Web is a Next.js app with a Node server (`server.ts`) and a WebSocket
layer over the GitHub Copilot SDK. The repository also contains separate
sub-projects (`foundry-doctor/`, `foundry-rules/`, `foundry-agents/`,
`hosted-agent-kit/`, `jev-byom/`, `routing-project/`, `sample-agents/`). Each has
its own tooling and CI workflow; a sub-project's own instruction file wins for
files inside it (for example `foundry-doctor/CLAUDE.md`).

## Commands (web app, repository root)

| Task | Command |
| --- | --- |
| Install | `npm ci` |
| Typecheck | `npm run typecheck` |
| Lint | `npm run lint` |
| Unit tests | `npm test` (Vitest) |
| End-to-end tests | `npm run test:e2e` (Playwright; needs `npx playwright install chromium`) |
| Build | `npm run build` |
| Database | `npx prisma migrate deploy`, `npm run prisma:generate` |

CI (`.github/workflows/ci.yml`) runs migrate, typecheck, lint, unit tests, e2e
and build, in that order, against a Postgres service. Run typecheck, lint and
unit tests before proposing a change to the web app.

## Layout

- `src/app`: Next.js routes and API handlers.
- `src/server`: session manager, WebSocket server, GitHub-backed file system, permission modes.
- `src/components`, `src/lib`, `src/auth.ts`: UI, helpers, authentication.
- `prisma/`: schema and migrations. `tests/`, `e2e/`: unit and end-to-end tests.

## Rules

- Never read, print or commit secrets or `.env` files. `.env.example` lists the variables.
- Do not skip, weaken or delete a test to get a passing run.
- Do not force-push, and do not commit, push or open a pull request unless asked.
- Match the style of the surrounding code; do not add dependencies the task does not need.

## Plan, then implement

For work that needs design decisions, use the `egm-plan` skill, then
`egm-implement` on the approved design. See `docs/egm/README.md`. For a small,
clear fix, skip them.
