"use client";

import { useEffect, useState } from "react";
import { ModeSelector } from "@/components/sessions/mode-selector";
import type { McpServerType, SessionAgentDto, SessionDetailDto, SessionMcpServerDto, SessionMode, SessionSkillDto } from "@/types/session";

/**
 * Per-session configuration: mode, custom agents (`CustomAgentConfig[]`),
 * skill toggles (`disabledSkills`), and MCP servers (`mcpServers` — stdio
 * or HTTP/SSE URLs, e.g. another team's internal MCP endpoint). Saved
 * independently per section so a mistake in one doesn't block the others;
 * all apply the next time the session's runtime (re)starts (see
 * SessionManager.ensureLive).
 */
export function SessionSettingsForm({ sessionId }: { sessionId: string }) {
    const [detail, setDetail] = useState<SessionDetailDto | null>(null);

    useEffect(() => {
        fetch(`/api/sessions/${sessionId}`)
            .then((res) => res.json())
            .then((data) => setDetail(data.session));
    }, [sessionId]);

    if (!detail) return <p className="p-4 text-sm text-muted">Loading…</p>;

    return (
        <div className="mx-auto max-w-lg space-y-8 p-4 pb-16">
            <ModeSection sessionId={sessionId} initialMode={detail.mode} />
            <AgentsSection sessionId={sessionId} initialAgents={detail.agents} />
            <SkillsSection sessionId={sessionId} initialSkills={detail.skills} />
            <McpServersSection sessionId={sessionId} initialServers={detail.mcpServers} />
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

function SaveButton({ onClick, savedLabel = "Saved" }: { onClick: () => Promise<void>; savedLabel?: string }) {
    const [state, setState] = useState<"idle" | "saving" | "saved">("idle");
    return (
        <button
            type="button"
            className="rounded-card bg-accent px-3 py-1.5 text-xs font-medium text-accent-foreground disabled:opacity-50"
            disabled={state === "saving"}
            onClick={async () => {
                setState("saving");
                await onClick();
                setState("saved");
                setTimeout(() => setState("idle"), 1500);
            }}
        >
            {state === "saving" ? "Saving…" : state === "saved" ? savedLabel : "Save"}
        </button>
    );
}

function ModeSection({ sessionId, initialMode }: { sessionId: string; initialMode: SessionMode }) {
    const [mode, setMode] = useState(initialMode);
    return (
        <Section title="Mode" hint="Applies the next time this session starts working.">
            <ModeSelector value={mode} onChange={setMode} />
            <SaveButton
                onClick={async () => {
                    await fetch(`/api/sessions/${sessionId}`, {
                        method: "PATCH",
                        headers: { "Content-Type": "application/json" },
                        body: JSON.stringify({ mode }),
                    });
                }}
            />
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
                <SaveButton onClick={async () => void (await fetch(`/api/sessions/${sessionId}/agents`, putJson({ agents })))} />
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
                <SaveButton onClick={async () => void (await fetch(`/api/sessions/${sessionId}/skills`, putJson({ skills })))} />
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
                    onClick={async () => {
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
                        await fetch(`/api/sessions/${sessionId}/mcp-servers`, putJson({ mcpServers }));
                    }}
                />
            </div>
        </Section>
    );
}

function putJson(body: unknown): RequestInit {
    return { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) };
}
