import { redirect } from "next/navigation";
import { auth } from "@/auth";
import { prisma } from "@/lib/prisma";
import { SessionList } from "@/components/sessions/session-list";
import type { SessionSummaryDto } from "@/types/session";

export default async function SessionsPage() {
    const session = await auth();
    if (!session?.user) redirect("/login");

    const rows = await prisma.session.findMany({
        where: { userId: session.user.id },
        orderBy: { lastActiveAt: "desc" },
    });

    const sessions: SessionSummaryDto[] = rows.map((row) => ({
        id: row.id,
        title: row.title,
        repoFullName: row.repoFullName,
        repoDefaultBranch: row.repoDefaultBranch,
        mode: row.mode as SessionSummaryDto["mode"],
        status: row.status as SessionSummaryDto["status"],
        createdAt: row.createdAt.toISOString(),
        updatedAt: row.updatedAt.toISOString(),
        lastActiveAt: row.lastActiveAt.toISOString(),
    }));

    return <SessionList sessions={sessions} userLogin={session.user.login} />;
}
