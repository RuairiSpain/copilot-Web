import { NextResponse } from "next/server";
import { z } from "zod";
import { auth } from "@/auth";
import { prisma } from "@/lib/prisma";

const agentSchema = z.object({
    name: z.string().min(1).max(64).regex(/^[a-zA-Z0-9_-]+$/),
    displayName: z.string().max(100).optional(),
    description: z.string().max(500).optional(),
    prompt: z.string().min(1).max(20_000),
    tools: z.array(z.string()).default([]),
});
const bodySchema = z.object({ agents: z.array(agentSchema) });

/** PUT /api/sessions/:id/agents — replaces this session's full custom
 * agent list, applied on the session's next (re)start
 * (SessionManager.ensureLive reads these rows into `customAgents`). */
export async function PUT(req: Request, { params }: { params: Promise<{ id: string }> }) {
    const session = await auth();
    if (!session?.user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    const { id } = await params;

    const owned = await prisma.session.findUnique({ where: { id }, select: { userId: true } });
    if (!owned || owned.userId !== session.user.id) return NextResponse.json({ error: "Not found" }, { status: 404 });

    const parsed = bodySchema.safeParse(await req.json());
    if (!parsed.success) return NextResponse.json({ error: parsed.error.flatten() }, { status: 400 });

    await prisma.$transaction([
        prisma.sessionAgent.deleteMany({ where: { sessionId: id } }),
        prisma.sessionAgent.createMany({
            data: parsed.data.agents.map((agent) => ({ sessionId: id, ...agent })),
        }),
    ]);

    return NextResponse.json({ ok: true });
}
