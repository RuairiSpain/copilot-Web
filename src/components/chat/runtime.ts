"use client";

import { useExternalStoreRuntime, type AppendMessage, type ThreadMessageLike } from "@assistant-ui/react";
import { useSessionSocket } from "./use-session-socket";
import { eventsToMessages, isSessionRunning } from "./events-to-messages";

/** Wires the session's WebSocket event stream into assistant-ui's
 * `useExternalStoreRuntime` (see nodejs docs — we own message state, the
 * runtime just renders it and forwards composer input back to us). */
export function useSessionRuntime(sessionId: string) {
    const socket = useSessionSocket(sessionId);
    const messages = eventsToMessages(socket.events);

    const runtime = useExternalStoreRuntime({
        messages,
        isRunning: isSessionRunning(socket.events),
        convertMessage: (message: ThreadMessageLike) => message,
        onNew: async (message: AppendMessage) => {
            const text = message.content
                .filter((part): part is { type: "text"; text: string } => part.type === "text")
                .map((part) => part.text)
                .join("\n");
            if (text.trim()) socket.sendPrompt(text);
        },
        onCancel: async () => socket.abort(),
    });

    return { runtime, socket, messages };
}
