"use client";

import { useEffect, useState } from "react";
import type { RepoSummary } from "@/types/session";

interface RepoPickerProps {
    value: RepoSummary | null;
    onChange: (repo: RepoSummary) => void;
}

/** Dropdown of the signed-in user's pushable repos, plus a "create new
 * public repo" affordance, used from the new-session dialog. */
export function RepoPicker({ value, onChange }: RepoPickerProps) {
    const [repos, setRepos] = useState<RepoSummary[] | null>(null);
    const [error, setError] = useState<string | null>(null);
    const [creating, setCreating] = useState(false);
    const [newRepoName, setNewRepoName] = useState("");
    const [busy, setBusy] = useState(false);

    useEffect(() => {
        fetch("/api/repos")
            .then((res) => {
                if (!res.ok) throw new Error("Failed to load repos");
                return res.json();
            })
            .then((data) => setRepos(data.repos))
            .catch((err) => setError(err.message));
    }, []);

    async function handleCreate() {
        if (!newRepoName.trim()) return;
        setBusy(true);
        setError(null);
        try {
            const res = await fetch("/api/repos/create", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ name: newRepoName.trim() }),
            });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error?.formErrors?.[0] ?? "Failed to create repo");
            setRepos((prev) => (prev ? [data.repo, ...prev] : [data.repo]));
            onChange(data.repo);
            setCreating(false);
            setNewRepoName("");
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to create repo");
        } finally {
            setBusy(false);
        }
    }

    return (
        <div className="space-y-2">
            <label className="text-sm font-medium">Repository</label>
            {error && <p className="text-sm text-danger">{error}</p>}

            {!creating ? (
                <>
                    <select
                        className="w-full rounded-card border border-border bg-surface px-3 py-2 text-sm"
                        value={value?.fullName ?? ""}
                        onChange={(e) => {
                            const repo = repos?.find((r) => r.fullName === e.target.value);
                            if (repo) onChange(repo);
                        }}
                        disabled={!repos}
                    >
                        <option value="" disabled>
                            {repos ? "Select a repo…" : "Loading repos…"}
                        </option>
                        {repos?.map((repo) => (
                            <option key={repo.fullName} value={repo.fullName}>
                                {repo.fullName}
                                {repo.private ? " (private)" : ""}
                            </option>
                        ))}
                    </select>
                    <button
                        type="button"
                        className="text-sm text-accent underline underline-offset-2"
                        onClick={() => setCreating(true)}
                    >
                        + Create a new public repo instead
                    </button>
                </>
            ) : (
                <div className="space-y-2 rounded-card border border-border p-3">
                    <input
                        className="w-full rounded-card border border-border bg-surface px-3 py-2 text-sm"
                        placeholder="my-new-repo"
                        value={newRepoName}
                        onChange={(e) => setNewRepoName(e.target.value)}
                    />
                    <div className="flex gap-2">
                        <button
                            type="button"
                            className="rounded-card bg-accent px-3 py-1.5 text-sm text-accent-foreground disabled:opacity-50"
                            onClick={handleCreate}
                            disabled={busy || !newRepoName.trim()}
                        >
                            {busy ? "Creating…" : "Create public repo"}
                        </button>
                        <button
                            type="button"
                            className="rounded-card px-3 py-1.5 text-sm text-muted"
                            onClick={() => setCreating(false)}
                        >
                            Cancel
                        </button>
                    </div>
                </div>
            )}
        </div>
    );
}
