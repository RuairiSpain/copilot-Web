"use client";

import { useState } from "react";

/** Delete requires this explicit warning dialog — the DELETE route itself
 * also rejects requests missing `{ confirm: true }` (see
 * api/sessions/[id]/route.ts), so this can't be bypassed by a stray
 * client-side call either. */
export function DeleteSessionDialog({
    sessionTitle,
    onCancel,
    onConfirm,
}: {
    sessionTitle: string;
    onCancel: () => void;
    onConfirm: () => Promise<void>;
}) {
    const [busy, setBusy] = useState(false);

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={onCancel}>
            <div className="w-full max-w-sm space-y-3 rounded-card bg-surface p-4" onClick={(e) => e.stopPropagation()}>
                <h2 className="text-lg font-semibold">Delete session?</h2>
                <p className="text-sm text-muted">
                    This permanently deletes <span className="font-medium text-foreground">{sessionTitle}</span> and
                    its full history. The repo itself is not affected. This can&apos;t be undone.
                </p>
                <div className="flex justify-end gap-2 pt-2">
                    <button type="button" className="rounded-card px-3 py-2 text-sm text-muted" onClick={onCancel} disabled={busy}>
                        Cancel
                    </button>
                    <button
                        type="button"
                        className="rounded-card bg-danger px-4 py-2 text-sm font-medium text-white disabled:opacity-50"
                        disabled={busy}
                        onClick={async () => {
                            setBusy(true);
                            await onConfirm();
                        }}
                    >
                        {busy ? "Deleting…" : "Delete session"}
                    </button>
                </div>
            </div>
        </div>
    );
}
