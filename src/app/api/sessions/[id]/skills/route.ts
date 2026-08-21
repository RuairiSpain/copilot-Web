import { NextResponse } from "next/server";
import { z } from "zod";
import { auth } from "@/auth";
import { prisma } from "@/lib/prisma";

const bodySchema = z.object({
    skills: z.array(z.object({ skillName: z.string().min(1).max(100), enabled: z.boolean() })),
});

/** PUT /api/sessions/:id/skills — replaces this session's skill toggle
 * list. Only disabled entries actually matter to the SDK
 * (`disabledSkills`); enabled ones are stored too so the settings UI can
 * show a consistent catalog across visits. */
export async function PUT(req: Request, { params }: { params: Promise<{ id: string }> }) {
    const session = await auth();
    if (!session?.user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    const { id } = await params;

    const owned = await prisma.session.findUnique({ where: { id }, select: { userId: true } });
    if (!owned || owned.userId !== session.user.id) return NextResponse.json({ error: "Not found" }, { status: 404 });

    const parsed = bodySchema.safeParse(await req.json());
    if (!parsed.success) return NextResponse.json({ error: parsed.error.flatten() }, { status: 400 });

    await prisma.$transaction([
        prisma.sessionSkill.deleteMany({ where: { sessionId: id } }),
        prisma.sessionSkill.createMany({
            data: parsed.data.skills.map((skill) => ({ sessionId: id, ...skill })),
        }),
    ]);

    return NextResponse.json({ ok: true });
}
