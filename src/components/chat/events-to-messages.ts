import type { ThreadMessageLike } from "@assistant-ui/react";
import type { WireEvent } from "@/types/session";

/**
 * Reduces the session's persisted/live event stream (SDK `SessionEvent`s
 * mirrored 1:1, plus this app's own `app.permission_requested` /
 * `app.plan_requested` synthetic events — see session-manager.ts) into
 * `ThreadMessageLike[]` for `useExternalStoreRuntime`.
 *
 * Event types consulted (see nodejs/docs/examples.md's "Top 10 event
 * types" table and samples/chat.ts): `user.message`, `assistant.message`,
 * `assistant.message_delta`, `assistant.reasoning`, `tool.execution_start`,
 * `tool.execution_complete`, `session.idle`, `session.error`.
 *
 * Permission/plan requests are represented as assistant messages with a
 * single synthetic `tool-call` part (`toolName: "__permission_request"` /
 * `"__plan_review"`) so the same content-part machinery renders them; see
 * PermissionCard/PlanCard in thread.tsx.
 *
 * Content parts are built as plain objects and cast to
 * `ThreadMessageLike["content"]` at the point they're attached to a
 * message — the shapes here line up with `@assistant-ui/react`'s real
 * `TextMessagePart`/`ReasoningMessagePart`/tool-call part, but TS can't
 * verify that our event `data` (persisted as loosely-typed JSON) is
 * actually JSON-safe all the way down, so the cast is a deliberate,
 * narrowly-scoped boundary rather than a blanket `any`.
 */

interface ToolCallPart {
    type: "tool-call";
    toolCallId: string;
    toolName: string;
    args: Record<string, unknown>;
    result?: unknown;
    isError?: boolean;
}
interface TextPart {
    type: "text";
    text: string;
}
interface ReasoningPart {
    type: "reasoning";
    text: string;
}
type MessagePart = TextPart | ReasoningPart | ToolCallPart;

function asContent(parts: MessagePart[]): ThreadMessageLike["content"] {
    return parts as unknown as ThreadMessageLike["content"];
}

interface Building {
    id: string;
    parts: MessagePart[];
    textIndex: number | null;
}

/** Accumulates one in-progress assistant turn at a time. A plain class
 * (rather than closures over a `let`) so mutating `current` across
 * branches of the event-type switch stays simple to reason about. */
class MessageBuilder {
    readonly messages: ThreadMessageLike[] = [];
    private readonly byId = new Map<string, ThreadMessageLike>();
    private current: Building | null = null;
    private readonly toolCallIndex = new Map<string, number>();

    ensure(seq: number): Building {
        if (!this.current) this.current = { id: `a-${seq}`, parts: [], textIndex: null };
        return this.current;
    }

    peek(): Building | null {
        return this.current;
    }

    flush(): void {
        if (this.current && this.current.parts.length > 0) {
            const id = this.current.id;
            const msg: ThreadMessageLike = { id, role: "assistant", content: asContent(this.current.parts) };
            this.messages.push(msg);
            this.byId.set(id, msg);
        }
        this.current = null;
        this.toolCallIndex.clear();
    }

    trackToolCall(toolCallId: string, index: number): void {
        this.toolCallIndex.set(toolCallId, index);
    }

    toolCallIndexOf(toolCallId: string): number | undefined {
        return this.toolCallIndex.get(toolCallId);
    }

    pushStandalone(id: string, part: ToolCallPart): void {
        this.flush();
        const msg: ThreadMessageLike = { id, role: "assistant", content: asContent([part]) };
        this.messages.push(msg);
        this.byId.set(id, msg);
    }

    patchStandalone(id: string, patch: Partial<ToolCallPart>): void {
        const msg = this.byId.get(id);
        const content = msg?.content;
        if (Array.isArray(content) && content[0] && (content[0] as ToolCallPart).type === "tool-call") {
            Object.assign(content[0] as ToolCallPart, patch);
        }
    }
}

export function eventsToMessages(events: WireEvent[]): ThreadMessageLike[] {
    const builder = new MessageBuilder();

    for (const event of events) {
        const data = (event.data ?? {}) as Record<string, unknown>;
        switch (event.type) {
            case "user.message":
                builder.flush();
                builder.messages.push({ id: `u-${event.seq}`, role: "user", content: String(data.content ?? "") });
                break;

            case "assistant.message_delta": {
                const b = builder.ensure(event.seq);
                if (b.textIndex === null) {
                    b.textIndex = b.parts.length;
                    b.parts.push({ type: "text", text: "" });
                }
                (b.parts[b.textIndex] as TextPart).text += String(data.deltaContent ?? "");
                break;
            }

            case "assistant.reasoning": {
                const b = builder.ensure(event.seq);
                b.parts.push({ type: "reasoning", text: String(data.content ?? "") });
                break;
            }

            case "assistant.message": {
                const b = builder.ensure(event.seq);
                const text = String(data.content ?? "");
                if (b.textIndex === null) {
                    b.textIndex = b.parts.length;
                    b.parts.push({ type: "text", text });
                } else {
                    (b.parts[b.textIndex] as TextPart).text = text;
                }
                builder.flush();
                break;
            }

            case "tool.execution_start": {
                const b = builder.ensure(event.seq);
                const toolCallId = String(data.toolCallId ?? event.seq);
                const idx = b.parts.length;
                b.parts.push({
                    type: "tool-call",
                    toolCallId,
                    toolName: String(data.toolName ?? "tool"),
                    args: (data.arguments as Record<string, unknown> | undefined) ?? {},
                });
                builder.trackToolCall(toolCallId, idx);
                break;
            }

            case "tool.execution_complete": {
                const toolCallId = String(data.toolCallId ?? "");
                const current = builder.peek();
                const idx = builder.toolCallIndexOf(toolCallId);
                if (current && idx !== undefined) {
                    const part = current.parts[idx] as ToolCallPart;
                    part.isError = data.success === false;
                    part.result = data.success === false ? (data.error ?? "Failed") : data.result;
                }
                break;
            }

            case "app.permission_requested":
                builder.pushStandalone(`perm-${String(data.requestId)}`, {
                    type: "tool-call",
                    toolCallId: String(data.requestId),
                    toolName: "__permission_request",
                    args: data,
                });
                break;
            case "app.permission_resolved":
                builder.patchStandalone(`perm-${String(data.requestId)}`, { result: data.decision });
                break;

            case "app.plan_requested":
                builder.pushStandalone(`plan-${String(data.requestId)}`, {
                    type: "tool-call",
                    toolCallId: String(data.requestId),
                    toolName: "__plan_review",
                    args: data,
                });
                break;
            case "app.plan_resolved":
                builder.patchStandalone(`plan-${String(data.requestId)}`, { result: data.approved ? "approved" : "rejected" });
                break;

            case "app.ask_user_requested":
                builder.pushStandalone(`ask-${String(data.requestId)}`, {
                    type: "tool-call",
                    toolCallId: String(data.requestId),
                    toolName: "__ask_user",
                    args: data,
                });
                break;
            case "app.ask_user_resolved":
                builder.patchStandalone(`ask-${String(data.requestId)}`, { result: data.answer });
                break;

            case "session.error":
                builder.flush();
                builder.messages.push({
                    id: `err-${event.seq}`,
                    role: "assistant",
                    content: asContent([{ type: "text", text: `⚠️ ${String(data.message ?? "An error occurred.")}` }]),
                });
                break;

            case "session.idle":
                builder.flush();
                break;

            default:
                break;
        }
    }
    builder.flush();
    return builder.messages;
}

/** A turn is "running" from the moment a prompt is sent until the session
 * next reports idle — used to disable the composer / show a stop button. */
export function isSessionRunning(events: WireEvent[]): boolean {
    let running = false;
    for (const event of events) {
        if (event.type === "user.message") running = true;
        else if (event.type === "session.idle") running = false;
    }
    return running;
}
