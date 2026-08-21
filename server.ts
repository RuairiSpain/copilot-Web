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
import { createServer } from "node:http";
import next from "next";
import { attachSessionWebSocketServer } from "./src/server/ws";

const port = Number(process.env.PORT ?? 3000);
const dev = process.env.NODE_ENV !== "production";

const app = next({ dev });
const handle = app.getRequestHandler();

async function main() {
    await app.prepare();

    const httpServer = createServer((req, res) => {
        handle(req, res);
    });

    attachSessionWebSocketServer(httpServer);

    httpServer.listen(port, () => {
        console.log(`> copilot-web listening on http://localhost:${port}`);
    });
}

main().catch((err) => {
    console.error("Failed to start server", err);
    process.exit(1);
});
