"use client";

import { useState } from "react";
import Link from "next/link";
import { NewSessionDialog } from "./new-session-dialog";
import type { SessionSummaryDto } from "@/types/session";

const MODE_LABEL: Record<SessionSummaryDto["mode"], string> = {
    planning: "Planning",
    interactive: "Interactive",
    auto: "Auto accept",
};

export function SessionList({ sessions }: { sessions: SessionSummaryDto[] }) {
    const [showNew, setShowNew] = useState(false);

    return (
        <div className="mx-auto flex min-h-dvh max-w-lg flex-col">
            <header className="flex items-center justify-between border-b border-border px-4 py-4">
                <h1 className="text-lg font-semibold">Sessions</h1>
                <button
                    type="button"
                    className="rounded-card bg-accent px-3 py-1.5 text-sm font-medium text-accent-foreground"
                    onClick={() => setShowNew(true)}
                >
                    + New
                </button>
            </header>

            <ul className="flex-1 divide-y divide-border overflow-y-auto">
                {sessions.length === 0 && (
                    <li className="px-4 py-10 text-center text-sm text-muted">
                        No sessions yet — start one to chat with Copilot in one of your repos.
                    </li>
                )}
                {sessions.map((session) => (
                    <li key={session.id}>
                        <Link href={`/sessions/${session.id}`} className="flex flex-col gap-1 px-4 py-3 active:bg-surface">
                            <div className="flex items-center justify-between">
                                <span className="font-medium">{session.title}</span>
                                <span className="rounded-full bg-surface px-2 py-0.5 text-[11px] text-muted">
                                    {MODE_LABEL[session.mode]}
                                </span>
                            </div>
                            <span className="text-xs text-muted">{session.repoFullName}</span>
                        </Link>
                    </li>
                ))}
            </ul>

            {showNew && <NewSessionDialog onClose={() => setShowNew(false)} />}
        </div>
    );
}
