import { notFound, redirect } from "next/navigation";
import Link from "next/link";
import { auth } from "@/auth";
import { prisma } from "@/lib/prisma";
import { SessionSettingsForm } from "@/components/settings/session-settings-form";

export default async function SessionSettingsPage({ params }: { params: Promise<{ id: string }> }) {
    const session = await auth();
    if (!session?.user) redirect("/login");
    const { id } = await params;

    const row = await prisma.session.findUnique({ where: { id }, select: { userId: true, title: true } });
    if (!row || row.userId !== session.user.id) notFound();

    return (
        <div className="min-h-dvh">
            <header className="flex items-center gap-2 border-b border-border px-3 py-2">
                <Link href={`/sessions/${id}`} className="rounded-card px-2 py-1 text-sm text-muted" aria-label="Back to chat">
                    ←
                </Link>
                <p className="text-sm font-medium">{row.title} — settings</p>
            </header>
            <SessionSettingsForm sessionId={id} />
        </div>
    );
}
