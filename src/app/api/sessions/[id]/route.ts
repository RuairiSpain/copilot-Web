import { NextResponse } from "next/server";
import { z } from "zod";
import { auth } from "@/auth";
import { prisma } from "@/lib/prisma";
import { sessionManager } from "@/server/session-manager";
import type { SessionDetailDto } from "@/types/session";

/** GET /api/sessions/:id — full detail for the settings screen (agents,
 * skills, MCP servers) and to render the mode/title header on the chat
 * screen before the WebSocket backlog has arrived. */
export async function GET(_req: Request, { params }: { params: Promise<{ id: string }> }) {
    const session = await auth();
    if (!session?.user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    const { id } = await params;

    const row = await prisma.session.findUnique({
        where: { id },
        include: { agents: true, skills: true, mcpServers: true },
    });
    if (!row || row.userId !== session.user.id) {
        return NextResponse.json({ error: "Not found" }, { status: 404 });
    }

    const dto: SessionDetailDto = {
        id: row.id,
        title: row.title,
        repoFullName: row.repoFullName,
        repoDefaultBranch: row.repoDefaultBranch,
        mode: row.mode as SessionDetailDto["mode"],
        status: row.status as SessionDetailDto["status"],
        createdAt: row.createdAt.toISOString(),
        updatedAt: row.updatedAt.toISOString(),
        lastActiveAt: row.lastActiveAt.toISOString(),
        agents: row.agents.map((a) => ({
            name: a.name,
            displayName: a.displayName ?? undefined,
            description: a.description ?? undefined,
            prompt: a.prompt,
            tools: a.tools,
        })),
        skills: row.skills.map((s) => ({ skillName: s.skillName, enabled: s.enabled })),
        mcpServers: row.mcpServers.map((m) => ({
            name: m.name,
            type: m.type as SessionDetailDto["mcpServers"][number]["type"],
            target: m.target,
            hasSecret: Boolean(m.encryptedConfig),
        })),
    };
    return NextResponse.json({ session: dto });
}

const patchSchema = z.object({
    title: z.string().min(1).max(200).optional(),
    mode: z.enum(["planning", "interactive", "auto"]).optional(),
});

/** PATCH /api/sessions/:id — rename or change mode. A mode change only
 * takes effect the next time the session is (re)attached — see
 * SessionManager.ensureLive, which reads `mode` fresh from the DB row each
 * time it starts the runtime — so an already-live session finishes its
 * current run under the old mode. */
export async function PATCH(req: Request, { params }: { params: Promise<{ id: string }> }) {
    const session = await auth();
    if (!session?.user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    const { id } = await params;

    const parsed = patchSchema.safeParse(await req.json());
    if (!parsed.success) return NextResponse.json({ error: parsed.error.flatten() }, { status: 400 });

    const existing = await prisma.session.findUnique({ where: { id } });
    if (!existing || existing.userId !== session.user.id) {
        return NextResponse.json({ error: "Not found" }, { status: 404 });
    }

    await prisma.session.update({ where: { id }, data: parsed.data });
    return NextResponse.json({ ok: true });
}

const deleteSchema = z.object({ confirm: z.literal(true) });

/** DELETE /api/sessions/:id — requires an explicit `{ confirm: true }`
 * body so this can only be reached after the UI's own warning dialog. */
export async function DELETE(req: Request, { params }: { params: Promise<{ id: string }> }) {
    const session = await auth();
    if (!session?.user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    const { id } = await params;

    const parsed = deleteSchema.safeParse(await req.json().catch(() => ({})));
    if (!parsed.success) {
        return NextResponse.json({ error: "Deletion requires { confirm: true }" }, { status: 400 });
    }

    try {
        await sessionManager.deleteSession(id, session.user.id);
    } catch (err) {
        return NextResponse.json({ error: err instanceof Error ? err.message : "Failed to delete" }, { status: 400 });
    }
    return NextResponse.json({ ok: true });
}
