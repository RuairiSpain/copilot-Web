"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { DeleteSessionDialog } from "./delete-session-dialog";
import type { SessionSummaryDto } from "@/types/session";

const MODE_LABEL: Record<SessionSummaryDto["mode"], string> = {
    planning: "Planning",
    interactive: "Interactive",
    auto: "Auto accept",
};

export function SessionHeader({ session }: { session: SessionSummaryDto }) {
    const router = useRouter();
    const [showDelete, setShowDelete] = useState(false);

    async function handleDelete() {
        const res = await fetch(`/api/sessions/${session.id}`, {
            method: "DELETE",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ confirm: true }),
        });
        if (res.ok) router.push("/sessions");
    }

    return (
        <header className="flex items-center justify-between gap-2 border-b border-border px-3 py-2">
            <div className="flex min-w-0 items-center gap-2">
                <Link href="/sessions" className="shrink-0 rounded-card px-2 py-1 text-sm text-muted" aria-label="Back to sessions">
                    ←
                </Link>
                <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{session.title}</p>
                    <p className="truncate text-xs text-muted">
                        {session.repoFullName} · {MODE_LABEL[session.mode]}
                    </p>
                </div>
            </div>
            <div className="flex shrink-0 items-center gap-1">
                <Link href={`/sessions/${session.id}/settings`} className="rounded-card px-2 py-1 text-sm text-muted" aria-label="Session settings">
                    ⚙
                </Link>
                <button
                    type="button"
                    className="rounded-card px-2 py-1 text-sm text-danger"
                    aria-label="Delete session"
                    onClick={() => setShowDelete(true)}
                >
                    🗑
                </button>
            </div>
            {showDelete && (
                <DeleteSessionDialog sessionTitle={session.title} onCancel={() => setShowDelete(false)} onConfirm={handleDelete} />
            )}
        </header>
    );
}
