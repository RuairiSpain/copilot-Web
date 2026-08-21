import { notFound, redirect } from "next/navigation";
import { auth } from "@/auth";
import { prisma } from "@/lib/prisma";
import { SessionHeader } from "@/components/sessions/session-header";
import { SessionThread } from "@/components/chat/thread";
import type { SessionSummaryDto } from "@/types/session";

export default async function SessionPage({ params }: { params: Promise<{ id: string }> }) {
    const session = await auth();
    if (!session?.user) redirect("/login");
    const { id } = await params;

    const row = await prisma.session.findUnique({ where: { id } });
    if (!row || row.userId !== session.user.id) notFound();

    const dto: SessionSummaryDto = {
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

    return (
        <div className="flex h-dvh flex-col">
            <SessionHeader session={dto} />
            <div className="min-h-0 flex-1">
                <SessionThread sessionId={id} />
            </div>
        </div>
    );
}
