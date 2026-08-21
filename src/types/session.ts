/** Shared DTOs between server routes and client components. Deliberately
 * decoupled from the Prisma model shape (dates as ISO strings, no relation
 * objects) so client bundles never need Prisma types. */

export type SessionMode = "planning" | "interactive" | "auto";
export type SessionStatus = "idle" | "running" | "archived";

export interface RepoSummary {
    fullName: string;
    private: boolean;
    defaultBranch: string;
    description: string | null;
    updatedAt: string | null;
}

export interface SessionAgentDto {
    name: string;
    displayName?: string;
    description?: string;
    prompt: string;
    tools: string[];
}

export interface SessionSkillDto {
    skillName: string;
    enabled: boolean;
}

export type McpServerType = "stdio" | "http";

export interface SessionMcpServerDto {
    name: string;
    type: McpServerType;
    target: string;
    /** Only ever present on write; the server never echoes secrets back. */
    hasSecret?: boolean;
}

export interface SessionSummaryDto {
    id: string;
    title: string;
    repoFullName: string;
    repoDefaultBranch: string;
    mode: SessionMode;
    status: SessionStatus;
    createdAt: string;
    updatedAt: string;
    lastActiveAt: string;
}

export interface SessionDetailDto extends SessionSummaryDto {
    agents: SessionAgentDto[];
    skills: SessionSkillDto[];
    mcpServers: SessionMcpServerDto[];
}

export interface CreateSessionInput {
    repoFullName: string;
    repoDefaultBranch: string;
    title?: string;
    mode: SessionMode;
}

/** Events the client cares about, mirroring the subset of SDK
 * `SessionEvent`s (session.ts) the UI renders, plus our own permission
 * round-trip messages that don't exist as SDK events. Kept intentionally
 * small/flat — this is the wire format over the session WebSocket.
 *
 * Tagged with `kind` rather than overloading `type` (WireEvent.type is a
 * plain `string`, not a literal, so a shared `type` discriminant doesn't
 * actually narrow the union). */
export type ClientSessionEvent = { kind: "backlog"; events: WireEvent[]; lastSeq: number } | { kind: "event"; event: WireEvent };

export interface WireEvent {
    seq: number;
    type: string;
    data: unknown;
    createdAt: string;
}

/** Matches the SDK's real `PermissionDecisionApproveForSession["kind"]`
 * literal (`"approve-for-session"`, not `"approve-session"`) — see
 * `PermissionDecision` in `@github/copilot-sdk`'s generated RPC types. */
export type PermissionDecisionKind = "approve-once" | "approve-for-session" | "reject";

/** Client -> server messages over the session WebSocket. */
export type ClientToServerMessage =
    | { kind: "prompt"; text: string }
    | { kind: "permission.respond"; requestId: string; decision: PermissionDecisionKind }
    | { kind: "plan.respond"; requestId: string; approved: boolean; feedback?: string }
    | { kind: "abort" };
