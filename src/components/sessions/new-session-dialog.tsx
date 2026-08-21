"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { RepoPicker } from "./repo-picker";
import { ModeSelector } from "./mode-selector";
import type { RepoSummary, SessionMode } from "@/types/session";

export function NewSessionDialog({ onClose }: { onClose: () => void }) {
    const router = useRouter();
    const [repo, setRepo] = useState<RepoSummary | null>(null);
    const [mode, setMode] = useState<SessionMode>("interactive");
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<string | null>(null);

    async function handleCreate() {
        if (!repo) return;
        setBusy(true);
        setError(null);
        try {
            const res = await fetch("/api/sessions", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ repoFullName: repo.fullName, repoDefaultBranch: repo.defaultBranch, mode }),
            });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error ?? "Failed to create session");
            router.push(`/sessions/${data.session.id}`);
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to create session");
            setBusy(false);
        }
    }

    return (
        <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/40 sm:items-center" onClick={onClose}>
            <div
                className="w-full max-w-md space-y-4 rounded-t-card bg-surface p-4 sm:rounded-card"
                onClick={(e) => e.stopPropagation()}
            >
                <h2 className="text-lg font-semibold">New session</h2>
                {error && <p className="text-sm text-danger">{error}</p>}
                <RepoPicker value={repo} onChange={setRepo} />
                <ModeSelector value={mode} onChange={setMode} />
                <div className="flex justify-end gap-2 pt-2">
                    <button type="button" className="rounded-card px-3 py-2 text-sm text-muted" onClick={onClose}>
                        Cancel
                    </button>
                    <button
                        type="button"
                        className="rounded-card bg-accent px-4 py-2 text-sm font-medium text-accent-foreground disabled:opacity-50"
                        disabled={!repo || busy}
                        onClick={handleCreate}
                    >
                        {busy ? "Creating…" : "Start session"}
                    </button>
                </div>
            </div>
        </div>
    );
}
