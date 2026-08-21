import { NextResponse } from "next/server";
import { z } from "zod";
import { auth } from "@/auth";
import { prisma } from "@/lib/prisma";
import { encryptSecret } from "@/lib/crypto";

const mcpServerSchema = z.object({
    name: z.string().min(1).max(64).regex(/^[a-zA-Z0-9_-]+$/),
    type: z.enum(["stdio", "http"]),
    target: z.string().min(1).max(2000), // command for stdio, URL for http
    headers: z.record(z.string(), z.string()).optional(),
    args: z.array(z.string()).optional(),
    env: z.record(z.string(), z.string()).optional(),
});
const bodySchema = z.object({ mcpServers: z.array(mcpServerSchema) });

/** PUT /api/sessions/:id/mcp-servers — replaces this session's MCP server
 * list. Secret-bearing fields (headers/args/env, which may carry bearer
 * tokens) are write-only: this form always sends the full set on save, and
 * they're encrypted at rest (src/lib/crypto.ts) before being stored — the
 * GET /api/sessions/:id detail route never echoes them back, only a
 * `hasSecret` flag. */
export async function PUT(req: Request, { params }: { params: Promise<{ id: string }> }) {
    const session = await auth();
    if (!session?.user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    const { id } = await params;

    const owned = await prisma.session.findUnique({ where: { id }, select: { userId: true } });
    if (!owned || owned.userId !== session.user.id) return NextResponse.json({ error: "Not found" }, { status: 404 });

    const parsed = bodySchema.safeParse(await req.json());
    if (!parsed.success) return NextResponse.json({ error: parsed.error.flatten() }, { status: 400 });

    await prisma.$transaction([
        prisma.sessionMcpServer.deleteMany({ where: { sessionId: id } }),
        prisma.sessionMcpServer.createMany({
            data: parsed.data.mcpServers.map((server) => {
                const extra = { headers: server.headers, args: server.args, env: server.env };
                const hasExtra = Object.values(extra).some((v) => v !== undefined && Object.keys(v).length > 0);
                return {
                    sessionId: id,
                    name: server.name,
                    type: server.type,
                    target: server.target,
                    encryptedConfig: hasExtra ? encryptSecret(JSON.stringify(extra)) : null,
                };
            }),
        }),
    ]);

    return NextResponse.json({ ok: true });
}
