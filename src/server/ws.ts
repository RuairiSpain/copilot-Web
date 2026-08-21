import type { Server as HttpServer, IncomingMessage } from "node:http";
import { WebSocketServer, type WebSocket } from "ws";
import { getToken } from "next-auth/jwt";
import { sessionManager } from "./session-manager";
import type { ClientSessionEvent, ClientToServerMessage } from "@/types/session";

/**
 * One WebSocket per attached session, at `/ws/sessions/:id`, sharing the
 * same HTTP server/process as Next.js (see server.ts) — not a separate
 * service, so a session's live CopilotClient and its connected clients
 * stay in the same process without a pub/sub layer between them.
 */

function toWebRequest(req: IncomingMessage): Request {
    const host = req.headers.host ?? "localhost";
    const url = `http://${host}${req.url ?? ""}`;
    const headers = new Headers();
    for (const [key, value] of Object.entries(req.headers)) {
        if (value === undefined) continue;
        headers.set(key, Array.isArray(value) ? value.join(", ") : value);
    }
    return new Request(url, { headers });
}

export function attachSessionWebSocketServer(httpServer: HttpServer) {
    const wss = new WebSocketServer({ noServer: true });

    httpServer.on("upgrade", (req, socket, head) => {
        const url = new URL(req.url ?? "", "http://internal");
        const match = url.pathname.match(/^\/ws\/sessions\/([^/]+)$/);
        if (!match) {
            socket.destroy();
            return;
        }
        const sessionId = match[1]!;
        const sinceSeq = Number(url.searchParams.get("since") ?? "0") || 0;

        void (async () => {
            const secret = process.env.AUTH_SECRET;
            if (!secret) {
                socket.destroy();
                return;
            }
            const token = await getToken({ req: toWebRequest(req), secret }).catch(() => null);
            const userId = token?.userId as string | undefined;
            if (!userId) {
                socket.write("HTTP/1.1 401 Unauthorized\r\n\r\n");
                socket.destroy();
                return;
            }

            wss.handleUpgrade(req, socket, head, (ws) => {
                void handleConnection(ws, sessionId, userId, sinceSeq);
            });
        })();
    });
}

async function handleConnection(ws: WebSocket, sessionId: string, userId: string, sinceSeq: number) {
    try {
        const backlog = await sessionManager.attach(sessionId, userId, ws, sinceSeq);
        const lastSeq = backlog.length > 0 ? backlog[backlog.length - 1]!.seq : sinceSeq;
        const backlogMessage: ClientSessionEvent = { kind: "backlog", events: backlog, lastSeq };
        ws.send(JSON.stringify(backlogMessage));
    } catch (err) {
        ws.close(4404, err instanceof Error ? err.message : "Failed to attach to session");
        return;
    }

    ws.on("message", (raw) => {
        let message: ClientToServerMessage;
        try {
            message = JSON.parse(raw.toString());
        } catch {
            return;
        }
        sessionManager.handleClientMessage(sessionId, message).catch((err) => {
            console.error(`Error handling message for session ${sessionId}`, err);
        });
    });

    ws.on("close", () => sessionManager.detach(sessionId, ws));
}
