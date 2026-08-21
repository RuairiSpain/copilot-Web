"use client";

import { useEffect, useState } from "react";
import { ModeSelector } from "@/components/sessions/mode-selector";
import type {
    McpServerType,
    SessionAgentDto,
    SessionDetailDto,
    SessionFunctionDto,
    SessionMcpServerDto,
    SessionMode,
    SessionSkillDto,
} from "@/types/session";

/**
 * Per-session configuration: mode, custom agents (`CustomAgentConfig[]`),
 * skill toggles (`disabledSkills`), MCP servers (`mcpServers` — stdio or
 * HTTP/SSE URLs, e.g. another team's internal MCP endpoint), and custom
 * "functions" (webhook-backed tools — see SessionFunction). Saved
 * independently per section so a mistake in one doesn't block the others;
 * all apply the next time the session's runtime (re)starts (see
 * SessionManager.ensureLive).
 */
export function SessionSettingsForm({ sessionId }: { sessionId: string }) {
    const [detail, setDetail] = useState<SessionDetailDto | null>(null);
    const [loadError, setLoadError] = useState<string | null>(null);

    useEffect(() => {
        fetch(`/api/sessions/${sessionId}`)
            .then((res) => {
                if (!res.ok) throw new Error("Failed to load session settings");
                return res.json();
            })
            .then((data) => setDetail(data.session))
            .catch((err) => setLoadError(err instanceof Error ? err.message : "Failed to load session settings"));
    }, [sessionId]);

    if (loadError) return <p className="p-4 text-sm text-danger">{loadError}</p>;
    if (!detail) return <p className="p-4 text-sm text-muted">Loading…</p>;

    return (
        <div className="mx-auto max-w-lg space-y-8 p-4 pb-16">
            <ModeSection sessionId={sessionId} initialMode={detail.mode} />
            <AgentsSection sessionId={sessionId} initialAgents={detail.agents} />
            <SkillsSection sessionId={sessionId} initialSkills={detail.skills} />
            <McpServersSection sessionId={sessionId} initialServers={detail.mcpServers} />
            <FunctionsSection sessionId={sessionId} initialFunctions={detail.functions} />
        </div>
    );
}

function Section({ title, hint, children }: { title: string; hint?: string; children: React.ReactNode }) {
    return (
        <section className="space-y-3">
            <div>
                <h2 className="text-sm font-semibold">{title}</h2>
                {hint && <p className="text-xs text-muted">{hint}</p>}
            </div>
            {children}
        </section>
    );
}

/** `onClick` returns whether the save actually succeeded — this used to
 * always flash "Saved" even when the underlying `fetch` failed. */
function SaveButton({ onClick, savedLabel = "Saved" }: { onClick: () => Promise<boolean>; savedLabel?: string }) {
    const [state, setState] = useState<"idle" | "saving" | "saved" | "error">("idle");
    return (
        <div className="flex items-center gap-2">
            <button
                type="button"
                className="rounded-card bg-accent px-3 py-1.5 text-xs font-medium text-accent-foreground disabled:opacity-50"
                disabled={state === "saving"}
                onClick={async () => {
                    setState("saving");
                    const ok = await onClick().catch(() => false);
                    setState(ok ? "saved" : "error");
                    setTimeout(() => setState("idle"), ok ? 1500 : 4000);
                }}
            >
                {state === "saving" ? "Saving…" : state === "saved" ? savedLabel : "Save"}
            </button>
            {state === "error" && <span className="text-xs text-danger">Failed to save — try again.</span>}
        </div>
    );
}

/** Wraps a PUT/PATCH `fetch` and returns whether it succeeded, for
 * `SaveButton`. */
async function saveRequest(url: string, body: unknown, method: "PUT" | "PATCH" = "PUT"): Promise<boolean> {
    try {
        const res = await fetch(url, { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
        return res.ok;
    } catch {
        return false;
    }
}

function ModeSection({ sessionId, initialMode }: { sessionId: string; initialMode: SessionMode }) {
    const [mode, setMode] = useState(initialMode);
    return (
        <Section title="Mode" hint="Applies the next time this session starts working.">
            <ModeSelector value={mode} onChange={setMode} />
            <SaveButton onClick={() => saveRequest(`/api/sessions/${sessionId}`, { mode }, "PATCH")} />
        </Section>
    );
}

function AgentsSection({ sessionId, initialAgents }: { sessionId: string; initialAgents: SessionAgentDto[] }) {
    const [agents, setAgents] = useState(initialAgents);

    return (
        <Section title="Agents" hint="Custom subagents this session can delegate to, each with its own prompt and tool list.">
            {agents.map((agent, i) => (
                <div key={i} className="space-y-2 rounded-card border border-border p-3">
                    <input
                        className="w-full rounded-card border border-border bg-background px-2 py-1 text-sm"
                        placeholder="name (e.g. code-reviewer)"
                        value={agent.name}
                        onChange={(e) => setAgents((a) => a.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))}
                    />
                    <textarea
                        className="w-full rounded-card border border-border bg-background px-2 py-1 text-sm"
                        placeholder="Prompt"
                        rows={3}
                        value={agent.prompt}
                        onChange={(e) => setAgents((a) => a.map((x, j) => (j === i ? { ...x, prompt: e.target.value } : x)))}
                    />
                    <input
                        className="w-full rounded-card border border-border bg-background px-2 py-1 text-sm"
                        placeholder="tools, comma separated (blank = all)"
                        value={agent.tools.join(", ")}
                        onChange={(e) =>
                            setAgents((a) =>
                                a.map((x, j) =>
                                    j === i ? { ...x, tools: e.target.value.split(",").map((t) => t.trim()).filter(Boolean) } : x,
                                ),
                            )
                        }
                    />
                    <button type="button" className="text-xs text-danger" onClick={() => setAgents((a) => a.filter((_, j) => j !== i))}>
                        Remove
                    </button>
                </div>
            ))}
            <div className="flex items-center gap-2">
                <button
                    type="button"
                    className="rounded-card border border-border px-3 py-1.5 text-xs"
                    onClick={() => setAgents((a) => [...a, { name: "", prompt: "", tools: [] }])}
                >
                    + Add agent
                </button>
                <SaveButton onClick={() => saveRequest(`/api/sessions/${sessionId}/agents`, { agents })} />
            </div>
        </Section>
    );
}

function SkillsSection({ sessionId, initialSkills }: { sessionId: string; initialSkills: SessionSkillDto[] }) {
    const [skills, setSkills] = useState(initialSkills);
    return (
        <Section title="Skills" hint="Toggle skills off to keep them out of this session's context.">
            {skills.map((skill, i) => (
                <label key={i} className="flex items-center justify-between rounded-card border border-border px-3 py-2 text-sm">
                    {skill.skillName}
                    <input
                        type="checkbox"
                        checked={skill.enabled}
                        onChange={(e) => setSkills((s) => s.map((x, j) => (j === i ? { ...x, enabled: e.target.checked } : x)))}
                    />
                </label>
            ))}
            <div className="flex items-center gap-2">
                <button
                    type="button"
                    className="rounded-card border border-border px-3 py-1.5 text-xs"
                    onClick={() => {
                        const name = prompt("Skill name");
                        if (name) setSkills((s) => [...s, { skillName: name, enabled: true }]);
                    }}
                >
                    + Add skill
                </button>
                <SaveButton onClick={() => saveRequest(`/api/sessions/${sessionId}/skills`, { skills })} />
            </div>
        </Section>
    );
}

function McpServersSection({ sessionId, initialServers }: { sessionId: string; initialServers: SessionMcpServerDto[] }) {
    type EditableServer = SessionMcpServerDto & { headers?: string };
    const [servers, setServers] = useState<EditableServer[]>(initialServers);

    return (
        <Section title="MCP servers" hint="Connect another service by its MCP URL (or a local stdio command).">
            {servers.map((server, i) => (
                <div key={i} className="space-y-2 rounded-card border border-border p-3">
                    <input
                        className="w-full rounded-card border border-border bg-background px-2 py-1 text-sm"
                        placeholder="name"
                        value={server.name}
                        onChange={(e) => setServers((s) => s.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))}
                    />
                    <select
                        className="w-full rounded-card border border-border bg-background px-2 py-1 text-sm"
                        value={server.type}
                        onChange={(e) => setServers((s) => s.map((x, j) => (j === i ? { ...x, type: e.target.value as McpServerType } : x)))}
                    >
                        <option value="http">HTTP / SSE URL</option>
                        <option value="stdio">Local command (stdio)</option>
                    </select>
                    <input
                        className="w-full rounded-card border border-border bg-background px-2 py-1 text-sm"
                        placeholder={server.type === "http" ? "https://example.com/mcp" : "command to run"}
                        value={server.target}
                        onChange={(e) => setServers((s) => s.map((x, j) => (j === i ? { ...x, target: e.target.value } : x)))}
                    />
                    {server.type === "http" && (
                        <input
                            className="w-full rounded-card border border-border bg-background px-2 py-1 text-sm"
                            placeholder={server.hasSecret ? "Authorization: Bearer •••• (leave blank to keep)" : "Authorization: Bearer <token> (optional)"}
                            onChange={(e) => setServers((s) => s.map((x, j) => (j === i ? { ...x, headers: e.target.value } : x)))}
                        />
                    )}
                    <button type="button" className="text-xs text-danger" onClick={() => setServers((s) => s.filter((_, j) => j !== i))}>
                        Remove
                    </button>
                </div>
            ))}
            <div className="flex items-center gap-2">
                <button
                    type="button"
                    className="rounded-card border border-border px-3 py-1.5 text-xs"
                    onClick={() => setServers((s) => [...s, { name: "", type: "http", target: "" }])}
                >
                    + Add MCP server
                </button>
                <SaveButton
                    onClick={() => {
                        const mcpServers = servers.map(({ name, type, target, headers }) => {
                            const [headerName, ...rest] = (headers ?? "").split(":");
                            const value = rest.join(":").trim();
                            return {
                                name,
                                type,
                                target,
                                headers: headerName && value ? { [headerName.trim()]: value } : undefined,
                            };
                        });
                        return saveRequest(`/api/sessions/${sessionId}/mcp-servers`, { mcpServers });
                    }}
                />
            </div>
        </Section>
    );
}

function FunctionsSection({ sessionId, initialFunctions }: { sessionId: string; initialFunctions: SessionFunctionDto[] }) {
    type EditableFunction = SessionFunctionDto & { authHeader?: string; parametersSchemaText: string };
    const [functions, setFunctions] = useState<EditableFunction[]>(
        initialFunctions.map((fn) => ({ ...fn, parametersSchemaText: JSON.stringify(fn.parametersSchema, null, 2) })),
    );
    const [schemaErrors, setSchemaErrors] = useState<Record<number, string>>({});

    return (
        <Section
            title="Functions"
            hint="Custom tools backed by a webhook: the agent calls the function, this app POSTs the arguments to your URL, and the response becomes the tool result."
        >
            {functions.map((fn, i) => (
                <div key={i} className="space-y-2 rounded-card border border-border p-3">
                    <input
                        className="w-full rounded-card border border-border bg-background px-2 py-1 text-sm"
                        placeholder="name (e.g. lookup_order)"
                        value={fn.name}
                        onChange={(e) => setFunctions((f) => f.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))}
                    />
                    <input
                        className="w-full rounded-card border border-border bg-background px-2 py-1 text-sm"
                        placeholder="Description shown to the agent"
                        value={fn.description}
                        onChange={(e) => setFunctions((f) => f.map((x, j) => (j === i ? { ...x, description: e.target.value } : x)))}
                    />
                    <input
                        className="w-full rounded-card border border-border bg-background px-2 py-1 text-sm"
                        placeholder="https://example.com/webhook"
                        value={fn.webhookUrl}
                        onChange={(e) => setFunctions((f) => f.map((x, j) => (j === i ? { ...x, webhookUrl: e.target.value } : x)))}
                    />
                    <textarea
                        className="w-full rounded-card border border-border bg-background px-2 py-1 font-mono text-xs"
                        rows={4}
                        placeholder={'{"type":"object","properties":{"orderId":{"type":"string"}},"required":["orderId"]}'}
                        value={fn.parametersSchemaText}
                        onChange={(e) => setFunctions((f) => f.map((x, j) => (j === i ? { ...x, parametersSchemaText: e.target.value } : x)))}
                    />
                    {schemaErrors[i] && <p className="text-xs text-danger">{schemaErrors[i]}</p>}
                    <input
                        className="w-full rounded-card border border-border bg-background px-2 py-1 text-sm"
                        placeholder={fn.hasSecret ? "Authorization: Bearer •••• (leave blank to keep)" : "Authorization: Bearer <token> (optional)"}
                        onChange={(e) => setFunctions((f) => f.map((x, j) => (j === i ? { ...x, authHeader: e.target.value } : x)))}
                    />
                    <button type="button" className="text-xs text-danger" onClick={() => setFunctions((f) => f.filter((_, j) => j !== i))}>
                        Remove
                    </button>
                </div>
            ))}
            <div className="flex items-center gap-2">
                <button
                    type="button"
                    className="rounded-card border border-border px-3 py-1.5 text-xs"
                    onClick={() =>
                        setFunctions((f) => [
                            ...f,
                            { name: "", description: "", webhookUrl: "", parametersSchema: {}, parametersSchemaText: '{\n  "type": "object",\n  "properties": {}\n}' },
                        ])
                    }
                >
                    + Add function
                </button>
                <SaveButton
                    onClick={() => {
                        const errors: Record<number, string> = {};
                        const parsed = functions.map((fn, i) => {
                            try {
                                return { ...fn, parametersSchema: JSON.parse(fn.parametersSchemaText || "{}") as Record<string, unknown> };
                            } catch {
                                errors[i] = "Invalid JSON Schema";
                                return fn;
                            }
                        });
                        setSchemaErrors(errors);
                        if (Object.keys(errors).length > 0) return Promise.resolve(false);

                        const payload = parsed.map(({ name, description, webhookUrl, parametersSchema, authHeader }) => {
                            const [headerName, ...rest] = (authHeader ?? "").split(":");
                            const value = rest.join(":").trim();
                            return {
                                name,
                                description,
                                webhookUrl,
                                parametersSchema,
                                headers: headerName && value ? { [headerName.trim()]: value } : undefined,
                            };
                        });
                        return saveRequest(`/api/sessions/${sessionId}/functions`, { functions: payload });
                    }}
                />
            </div>
        </Section>
    );
}
