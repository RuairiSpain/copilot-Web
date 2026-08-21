"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type { ClientSessionEvent, ClientToServerMessage, PermissionDecisionKind, WireEvent } from "@/types/session";

export interface SessionSocketState {
    events: WireEvent[];
    connected: boolean;
}

/**
 * Owns the WebSocket to `/ws/sessions/:id`. Reconnects with backoff and
 * always resumes from the last seq it has seen (`?since=`), so a dropped
 * connection — the phone going to sleep, a tab reload, or opening the same
 * session on a second device — replays only the gap rather than losing or
 * duplicating history.
 */
export function useSessionSocket(sessionId: string) {
    const [state, setState] = useState<SessionSocketState>({ events: [], connected: false });
    const wsRef = useRef<WebSocket | null>(null);
    const lastSeqRef = useRef(0);
    const reconnectDelayRef = useRef(1000);
    const closedByUsRef = useRef(false);

    useEffect(() => {
        closedByUsRef.current = false;

        function connect() {
            const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
            const ws = new WebSocket(`${protocol}//${window.location.host}/ws/sessions/${sessionId}?since=${lastSeqRef.current}`);
            wsRef.current = ws;

            ws.onopen = () => {
                reconnectDelayRef.current = 1000;
                setState((s) => ({ ...s, connected: true }));
            };

            ws.onmessage = (raw) => {
                const message: ClientSessionEvent = JSON.parse(raw.data);
                if (message.kind === "backlog") {
                    lastSeqRef.current = Math.max(lastSeqRef.current, message.lastSeq);
                    setState((s) => ({ ...s, events: dedupeAppend(s.events, message.events) }));
                    return;
                }
                lastSeqRef.current = Math.max(lastSeqRef.current, message.event.seq);
                setState((s) => ({ ...s, events: dedupeAppend(s.events, [message.event]) }));
            };

            ws.onclose = () => {
                setState((s) => ({ ...s, connected: false }));
                if (closedByUsRef.current) return;
                const delay = Math.min(reconnectDelayRef.current, 15000);
                reconnectDelayRef.current *= 2;
                setTimeout(connect, delay);
            };

            ws.onerror = () => ws.close();
        }

        connect();
        return () => {
            closedByUsRef.current = true;
            wsRef.current?.close();
        };
    }, [sessionId]);

    const send = useCallback((message: ClientToServerMessage) => {
        if (wsRef.current?.readyState === WebSocket.OPEN) wsRef.current.send(JSON.stringify(message));
    }, []);

    return {
        ...state,
        sendPrompt: (text: string) => send({ kind: "prompt", text }),
        abort: () => send({ kind: "abort" }),
        respondPermission: (requestId: string, decision: PermissionDecisionKind) =>
            send({ kind: "permission.respond", requestId, decision }),
        respondPlan: (requestId: string, approved: boolean, selectedAction?: string, feedback?: string) =>
            send({ kind: "plan.respond", requestId, approved, selectedAction, feedback }),
        respondAskUser: (requestId: string, answer: string, wasFreeform: boolean) =>
            send({ kind: "ask_user.respond", requestId, answer, wasFreeform }),
    };
}

function dedupeAppend(existing: WireEvent[], incoming: WireEvent[]): WireEvent[] {
    if (incoming.length === 0) return existing;
    const seen = new Set(existing.map((e) => e.seq));
    const merged = [...existing, ...incoming.filter((e) => !seen.has(e.seq))];
    merged.sort((a, b) => a.seq - b.seq);
    return merged;
}
