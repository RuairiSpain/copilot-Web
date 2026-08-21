import { prisma } from "@/lib/prisma";
import type { WireEvent } from "@/types/session";

/**
 * Append-only mirror of SDK SessionEvents (Prisma `SessionEvent` model).
 * This — not the SDK's own in-memory `getEvents()` — is what makes
 * sessions survive a server restart and lets a client reconnecting from a
 * different device replay history before subscribing live.
 */

export async function appendEvent(sessionId: string, type: string, data: unknown): Promise<WireEvent> {
    // Sequence numbers are assigned per-session inside a transaction so
    // concurrent events (e.g. rapid tool start/complete pairs) still land
    // in a gap-free, strictly increasing order for replay.
    const row = await prisma.$transaction(async (tx) => {
        const session = await tx.session.update({
            where: { id: sessionId },
            data: { lastEventSeq: { increment: 1 }, lastActiveAt: new Date() },
            select: { lastEventSeq: true },
        });
        return tx.sessionEvent.create({
            data: { sessionId, seq: session.lastEventSeq, type, data: data as object },
        });
    });

    return { seq: row.seq, type: row.type, data: row.data, createdAt: row.createdAt.toISOString() };
}

export async function getEventsSince(sessionId: string, sinceSeq: number, limit = 500): Promise<WireEvent[]> {
    const rows = await prisma.sessionEvent.findMany({
        where: { sessionId, seq: { gt: sinceSeq } },
        orderBy: { seq: "asc" },
        take: limit,
    });
    return rows.map((row) => ({ seq: row.seq, type: row.type, data: row.data, createdAt: row.createdAt.toISOString() }));
}
