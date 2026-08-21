import { NextResponse } from "next/server";
import { z } from "zod";
import type { Prisma } from "@prisma/client";
import { auth } from "@/auth";
import { prisma } from "@/lib/prisma";
import { encryptSecret } from "@/lib/crypto";

const functionSchema = z.object({
    name: z.string().min(1).max(64).regex(/^[a-zA-Z0-9_-]+$/),
    description: z.string().min(1).max(500),
    // A JSON Schema object (e.g. { type: "object", properties: {...} }).
    // Validated loosely here — the SDK/model is the real consumer, and a
    // malformed schema just makes that one function unusable, not unsafe.
    parametersSchema: z.record(z.string(), z.unknown()),
    webhookUrl: z.string().url(),
    headers: z.record(z.string(), z.string()).optional(),
});
const bodySchema = z.object({ functions: z.array(functionSchema) });

/** PUT /api/sessions/:id/functions — replaces this session's custom
 * "functions" (webhook-backed tools). Extra headers (e.g. an API key) are
 * write-only, like MCP server secrets — this form always sends the full
 * set on save. */
export async function PUT(req: Request, { params }: { params: Promise<{ id: string }> }) {
    const session = await auth();
    if (!session?.user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    const { id } = await params;

    const owned = await prisma.session.findUnique({ where: { id }, select: { userId: true } });
    if (!owned || owned.userId !== session.user.id) return NextResponse.json({ error: "Not found" }, { status: 404 });

    const parsed = bodySchema.safeParse(await req.json());
    if (!parsed.success) return NextResponse.json({ error: parsed.error.flatten() }, { status: 400 });

    await prisma.$transaction([
        prisma.sessionFunction.deleteMany({ where: { sessionId: id } }),
        prisma.sessionFunction.createMany({
            data: parsed.data.functions.map((fn) => ({
                sessionId: id,
                name: fn.name,
                description: fn.description,
                parametersSchema: fn.parametersSchema as Prisma.InputJsonValue,
                webhookUrl: fn.webhookUrl,
                encryptedHeaders: fn.headers && Object.keys(fn.headers).length > 0 ? encryptSecret(JSON.stringify({ headers: fn.headers })) : null,
            })),
        }),
    ]);

    return NextResponse.json({ ok: true });
}
