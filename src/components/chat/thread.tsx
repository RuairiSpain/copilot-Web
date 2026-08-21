"use client";

import { AssistantRuntimeProvider, ThreadPrimitive, ComposerPrimitive, type ThreadMessageLike } from "@assistant-ui/react";
import { useSessionRuntime } from "./runtime";
import { useSessionSocket } from "./use-session-socket";

/**
 * The session chat thread. `ThreadPrimitive`/`ComposerPrimitive` drive
 * scroll/composer/running-state behavior against the runtime; messages are
 * mapped directly from our own event-derived array (events-to-messages.ts,
 * returned by useSessionRuntime) rather than through
 * `ThreadPrimitive.Messages` — that primitive's `components` form renders
 * prop-less components reading from internal store context, and its
 * children-function form hands back the store's own `MessageState`, not
 * our `ThreadMessageLike[]`; mapping our own array directly is simpler and
 * exactly as correct for a list we already own.
 */
export function SessionThread({ sessionId }: { sessionId: string }) {
    const { runtime, socket, messages } = useSessionRuntime(sessionId);

    return (
        <AssistantRuntimeProvider runtime={runtime}>
            <ThreadPrimitive.Root className="flex h-full flex-col">
                <ThreadPrimitive.Viewport className="flex-1 space-y-3 overflow-y-auto px-4 py-4">
                    {messages.length === 0 && (
                        <p className="mt-10 text-center text-sm text-muted">
                            Send a message to get started. Copilot will work in this repo according to the
                            session&apos;s mode.
                        </p>
                    )}
                    {messages.map((message) =>
                        message.role === "user" ? (
                            <UserBubble key={message.id} message={message} />
                        ) : (
                            <AssistantBubble key={message.id} message={message} socket={socket} />
                        ),
                    )}
                </ThreadPrimitive.Viewport>
                <div className="pb-safe border-t border-border bg-surface px-3 py-3">
                    <ConnectionBanner connected={socket.connected} />
                    <Composer />
                </div>
            </ThreadPrimitive.Root>
        </AssistantRuntimeProvider>
    );
}

function ConnectionBanner({ connected }: { connected: boolean }) {
    if (connected) return null;
    return (
        <p className="mb-2 rounded-card bg-muted/10 px-3 py-1.5 text-center text-xs text-muted">
            Reconnecting… the session keeps working on the server even while you&apos;re offline.
        </p>
    );
}

function Composer() {
    return (
        <ComposerPrimitive.Root className="flex items-end gap-2">
            <ComposerPrimitive.Input
                rows={1}
                placeholder="Message Copilot…"
                className="max-h-40 flex-1 resize-none rounded-card border border-border bg-background px-3 py-2 text-sm outline-none"
            />
            <ThreadPrimitive.If running={false}>
                <ComposerPrimitive.Send className="rounded-card bg-accent px-4 py-2 text-sm font-medium text-accent-foreground disabled:opacity-50" />
            </ThreadPrimitive.If>
            <ThreadPrimitive.If running>
                <ComposerPrimitive.Cancel className="rounded-card bg-danger px-4 py-2 text-sm font-medium text-white" />
            </ThreadPrimitive.If>
        </ComposerPrimitive.Root>
    );
}

function UserBubble({ message }: { message: ThreadMessageLike }) {
    const text = typeof message.content === "string" ? message.content : "";
    return (
        <div className="ml-auto max-w-[85%] rounded-card bg-accent px-3 py-2 text-sm text-accent-foreground">
            {text}
        </div>
    );
}

function AssistantBubble({ message, socket }: { message: ThreadMessageLike; socket: ReturnType<typeof useSessionSocket> }) {
    const parts = Array.isArray(message.content) ? message.content : [];
    return (
        <div className="mr-auto max-w-[90%] space-y-2">
            {parts.map((part, i) => (
                <MessagePartView key={i} part={part} socket={socket} />
            ))}
        </div>
    );
}

function MessagePartView({ part, socket }: { part: unknown; socket: ReturnType<typeof useSessionSocket> }) {
    const p = part as { type: string; text?: string; toolName?: string; toolCallId?: string; args?: unknown; result?: unknown; isError?: boolean };

    if (p.type === "text") {
        return <div className="whitespace-pre-wrap rounded-card bg-surface px-3 py-2 text-sm shadow-sm">{p.text}</div>;
    }
    if (p.type === "reasoning") {
        return (
            <details className="rounded-card border border-dashed border-border px-3 py-2 text-xs text-muted">
                <summary className="cursor-pointer select-none">Reasoning</summary>
                <p className="mt-1 whitespace-pre-wrap">{p.text}</p>
            </details>
        );
    }
    if (p.type === "tool-call" && p.toolName === "__permission_request") {
        return <PermissionCard requestId={p.toolCallId!} args={p.args} result={p.result as string | undefined} socket={socket} />;
    }
    if (p.type === "tool-call" && p.toolName === "__plan_review") {
        return <PlanCard requestId={p.toolCallId!} args={p.args} result={p.result as string | undefined} socket={socket} />;
    }
    if (p.type === "tool-call") {
        return <ToolCard toolName={p.toolName!} args={p.args} result={p.result} isError={p.isError} />;
    }
    return null;
}

function ToolCard({ toolName, args, result, isError }: { toolName: string; args: unknown; result: unknown; isError?: boolean }) {
    const done = result !== undefined;
    return (
        <div className="rounded-card border border-border bg-surface px-3 py-2 text-xs">
            <div className="flex items-center gap-2 font-medium">
                <span className={done ? (isError ? "text-danger" : "text-accent") : "animate-pulse text-muted"}>●</span>
                {toolName}
                {!done && <span className="text-muted">running…</span>}
            </div>
            {args !== undefined && (
                <pre className="mt-1 overflow-x-auto text-[11px] text-muted">{JSON.stringify(args, null, 2)}</pre>
            )}
            {done && (
                <pre className="mt-1 overflow-x-auto text-[11px]">{typeof result === "string" ? result : JSON.stringify(result, null, 2)}</pre>
            )}
        </div>
    );
}

/** Interactive mode's core UI: every tool permission request from the
 * agent is rendered here and blocks until the user taps a decision (see
 * permission-modes.ts / SessionManager.handleClientMessage). */
function PermissionCard({
    requestId,
    args,
    result,
    socket,
}: {
    requestId: string;
    args: unknown;
    result?: string;
    socket: ReturnType<typeof useSessionSocket>;
}) {
    const request = args as { kind?: string; fullCommandText?: string; path?: string };
    if (result) {
        return (
            <div className="rounded-card border border-border bg-surface px-3 py-2 text-xs text-muted">
                Permission {result === "reject" ? "denied" : "approved"} — {request.kind ?? "tool"}
            </div>
        );
    }
    return (
        <div className="space-y-2 rounded-card border border-accent/40 bg-accent/5 px-3 py-2 text-sm">
            <p className="font-medium">Copilot wants to run: {request.kind ?? "a tool"}</p>
            {request.fullCommandText && <pre className="overflow-x-auto text-xs">{request.fullCommandText}</pre>}
            {request.path && <p className="text-xs text-muted">{request.path}</p>}
            <div className="flex gap-2">
                <button
                    className="rounded-card bg-accent px-3 py-1.5 text-xs font-medium text-accent-foreground"
                    onClick={() => socket.respondPermission(requestId, "approve-once")}
                >
                    Allow once
                </button>
                <button
                    className="rounded-card border border-border px-3 py-1.5 text-xs"
                    onClick={() => socket.respondPermission(requestId, "approve-for-session")}
                >
                    Allow for session
                </button>
                <button
                    className="rounded-card border border-danger px-3 py-1.5 text-xs text-danger"
                    onClick={() => socket.respondPermission(requestId, "reject")}
                >
                    Deny
                </button>
            </div>
        </div>
    );
}

/** Planning mode's core UI: gates leaving the CLI's read-only plan mode
 * until the user reviews and approves. */
function PlanCard({
    requestId,
    args,
    result,
    socket,
}: {
    requestId: string;
    args: unknown;
    result?: string;
    socket: ReturnType<typeof useSessionSocket>;
}) {
    const request = args as { plan?: string; summary?: string };
    if (result) {
        return (
            <div className="rounded-card border border-border bg-surface px-3 py-2 text-xs text-muted">
                Plan {result === "approved" ? "approved" : "sent back for changes"}
            </div>
        );
    }
    return (
        <div className="space-y-2 rounded-card border border-accent/40 bg-accent/5 px-3 py-2 text-sm">
            <p className="font-medium">Review plan</p>
            <div className="whitespace-pre-wrap rounded-card bg-surface px-2 py-1.5 text-xs">
                {request.plan ?? request.summary ?? "No plan details provided."}
            </div>
            <div className="flex gap-2">
                <button
                    className="rounded-card bg-accent px-3 py-1.5 text-xs font-medium text-accent-foreground"
                    onClick={() => socket.respondPlan(requestId, true)}
                >
                    Approve &amp; start
                </button>
                <button
                    className="rounded-card border border-border px-3 py-1.5 text-xs"
                    onClick={() => socket.respondPlan(requestId, false, "Please revise the plan.")}
                >
                    Request changes
                </button>
            </div>
        </div>
    );
}
