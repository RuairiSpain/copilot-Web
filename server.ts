/**
 * Custom Node.js server: hosts the Next.js request handler and the
 * session WebSocket server on a single HTTP server / single process.
 *
 * This is intentional, not incidental: @github/copilot-sdk spawns a native
 * runtime (koffi FFI) per CopilotClient and keeps live session state
 * in-process, so this app cannot run on serverless/edge — it needs exactly
 * one always-on Node process, which is also why session WebSocket upgrades
 * are attached directly to this server rather than routed through a
 * separate service.
 */
// Order matters: load-dotenv (loads .env locally) must run before
// bootstrap-env (synthesizes DATABASE_URL from discrete PG* env vars —
// Azure Container Apps deployments), which must run before anything
// below transitively instantiates PrismaClient.
import "./src/server/load-dotenv";
import "./src/server/bootstrap-env";
import { createServer } from "node:http";
import next from "next";
import { attachSessionWebSocketServer } from "./src/server/ws";
import { formatEnvProblems, validateEnv } from "./src/server/env";

const port = Number(process.env.PORT ?? 3000);
const dev = process.env.NODE_ENV !== "production";

const app = next({ dev });
const handle = app.getRequestHandler();

async function main() {
    // Fail fast with a clear message rather than the first login attempt
    // hitting an obscure error deep in NextAuth or src/lib/crypto.ts.
    const envProblems = validateEnv();
    if (envProblems.length > 0) {
        console.error(formatEnvProblems(envProblems));
        process.exit(1);
    }

    await app.prepare();

    const httpServer = createServer((req, res) => {
        handle(req, res);
    });

    attachSessionWebSocketServer(httpServer, app.getUpgradeHandler());

    httpServer.listen(port, () => {
        console.log(`> copilot-web listening on http://localhost:${port}`);
    });
}

main().catch((err) => {
    console.error("Failed to start server", err);
    process.exit(1);
});
