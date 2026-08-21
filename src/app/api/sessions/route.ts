import { NextResponse } from "next/server";
import { z } from "zod";
import { auth } from "@/auth";
import { prisma } from "@/lib/prisma";
import type { SessionSummaryDto } from "@/types/session";

function toSummaryDto(row: {
    id: string;
    title: string;
    repoFullName: string;
    repoDefaultBranch: string;
    mode: string;
    status: string;
    createdAt: Date;
    updatedAt: Date;
    lastActiveAt: Date;
}): SessionSummaryDto {
    return {
        id: row.id,
        title: row.title,
        repoFullName: row.repoFullName,
        repoDefaultBranch: row.repoDefaultBranch,
        mode: row.mode as SessionSummaryDto["mode"],
        status: row.status as SessionSummaryDto["status"],
        createdAt: row.createdAt.toISOString(),
        updatedAt: row.updatedAt.toISOString(),
        lastActiveAt: row.lastActiveAt.toISOString(),
    };
}

/** GET /api/sessions — every session belonging to the signed-in user,
 * across every device/login: sessions are keyed by our own DB, not by
 * browser/local storage, which is what makes them survive logging in
 * elsewhere. */
export async function GET() {
    const session = await auth();
    if (!session?.user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });

    const rows = await prisma.session.findMany({
        where: { userId: session.user.id },
        orderBy: { lastActiveAt: "desc" },
    });
    return NextResponse.json({ sessions: rows.map(toSummaryDto) });
}

const createSchema = z.object({
    repoFullName: z.string().regex(/^[^/]+\/[^/]+$/),
    repoDefaultBranch: z.string().min(1),
    title: z.string().max(200).optional(),
    mode: z.enum(["planning", "interactive", "auto"]),
});

/** POST /api/sessions — creates a new session row. The underlying
 * CopilotClient/CopilotSession is started lazily on first WebSocket attach
 * (see SessionManager.ensureLive), not here — creating a row is cheap and
 * shouldn't have to wait on spawning a runtime process. */
export async function POST(req: Request) {
    const session = await auth();
    if (!session?.user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });

    const parsed = createSchema.safeParse(await req.json());
    if (!parsed.success) return NextResponse.json({ error: parsed.error.flatten() }, { status: 400 });

    const row = await prisma.session.create({
        data: {
            userId: session.user.id,
            repoFullName: parsed.data.repoFullName,
            repoDefaultBranch: parsed.data.repoDefaultBranch,
            title: parsed.data.title?.trim() || parsed.data.repoFullName,
            mode: parsed.data.mode,
        },
    });

    return NextResponse.json({ session: toSummaryDto(row) }, { status: 201 });
}
